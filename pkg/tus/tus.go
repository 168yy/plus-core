package tus

import (
	"context"
	"github.com/168yy/plus-core/core/v2/logger"
	"github.com/gogf/gf/v2/net/ghttp"
	"io"
	"math"
	"strconv"
	"time"
)

func NewTus(config Config) (*Uploader, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	// Only promote extesions using the Tus-Extension header which are implemented
	extensions := "creation,creation-with-upload"
	if config.StoreComposer.UsesTerminater {
		extensions += ",termination"
	}
	if config.StoreComposer.UsesConcater {
		extensions += ",concatenation"
	}
	if config.StoreComposer.UsesLengthDeferrer {
		extensions += ",creation-defer-length"
	}

	t := &Uploader{
		config:            config,
		composer:          config.StoreComposer,
		basePath:          config.BasePath,
		isBasePathAbs:     config.IsAbs,
		CompleteUploads:   make(chan HookEvent),
		TerminatedUploads: make(chan HookEvent),
		UploadProgress:    make(chan HookEvent),
		CreatedUploads:    make(chan HookEvent),
		logger:            config.Logger,
		extensions:        extensions,
		Metrics:           newMetrics(),
	}
	return t, nil
}

type Uploader struct {
	config        Config
	composer      *StoreComposer
	isBasePathAbs bool
	basePath      string
	logger        logger.ILogger
	extensions    string

	// CompleteUploads is used to send notifications whenever an upload is
	// completed by a user. The HookEvent will contain information about this
	// upload after it is completed. Sending to this channel will only
	// happen if the NotifyCompleteUploads field is set to true in the Config
	// structure. Notifications will also be sent for completions using the
	// Concatenation extension.
	CompleteUploads chan HookEvent
	// TerminatedUploads is used to send notifications whenever an upload is
	// terminated by a user. The HookEvent will contain information about this
	// upload gathered before the termination. Sending to this channel will only
	// happen if the NotifyTerminatedUploads field is set to true in the Config
	// structure.
	TerminatedUploads chan HookEvent
	// UploadProgress is used to send notifications about the progress of the
	// currently running uploads. For each open PATCH request, every second
	// a HookEvent instance will be sent over this channel with the Offset field
	// being set to the number of bytes which have been transfered to the server.
	// Please be aware that this number may be higher than the number of bytes
	// which have been stored by the data store! Sending to this channel will only
	// happen if the NotifyUploadProgress field is set to true in the Config
	// structure.
	UploadProgress chan HookEvent
	// CreatedUploads is used to send notifications about the uploads having been
	// created. It triggers post creation and therefore has all the HookEvent incl.
	// the ID available already. It facilitates the post-create hook. Sending to
	// this channel will only happen if the NotifyCreatedUploads field is set to
	// true in the Config structure.
	CreatedUploads chan HookEvent
	// Metrics provides numbers of the usage for this handler.
	Metrics Metrics
}

// writeChunk reads the body from the requests r and appends it to the upload
// with the corresponding id. Afterward, it will set the necessary response
// headers but will not send the response.
func (u *Uploader) writeChunk(ctx context.Context, upload Upload, info FileInfo, r *ghttp.Request) error {
	// Get Content-Length if possible
	length := r.ContentLength
	offset := info.Offset
	id := info.ID

	// Test if this upload fits into the file's size
	if !info.SizeIsDeferred && offset+length > info.Size {
		return ErrSizeExceeded
	}

	maxSize := info.Size - offset
	// If the upload's length is deferred and the PATCH request does not contain the Content-Length
	// header (which is allowed if 'Transfer-Encoding: chunked' is used), we still need to set limits for
	// the body size.
	if info.SizeIsDeferred {
		if u.config.MaxSize > 0 {
			// Ensure that the upload does not exceed the maximum upload size
			maxSize = u.config.MaxSize - offset
		} else {
			// If no upload limit is given, we allow arbitrary sizes
			maxSize = math.MaxInt64
		}
	}
	if length > 0 {
		maxSize = length
	}

	u.log(ctx, "ChunkWriteStart", "id", id, "maxSize", i64toa(maxSize), "offset", i64toa(offset))

	var bytesWritten int64
	var err error
	// Prevent a nil pointer dereference when accessing the body which may not be
	// available in the case of a malicious request.
	if r.Body != nil && maxSize > 0 {
		// Limit the data read from the request's body to the allowed maximum
		reader := newBodyReader(io.LimitReader(r.Body, maxSize))

		// We use a context object to allow the hook system to cancel an upload
		uploadCtx, stopUpload := context.WithCancel(context.Background())
		info.stopUpload = stopUpload
		// terminateUpload specifies whether the upload should be deleted after
		// the writing has finished
		terminateUpload := false
		// Cancel the context when the function exits to ensure that the goroutine
		// is properly cleaned up
		defer stopUpload()

		go func() {
			// Interrupt the Read() call from the request body
			<-uploadCtx.Done()
			terminateUpload = true
			_ = r.Body.Close()
		}()

		if u.config.NotifyUploadProgress {
			stopProgressEvents := u.sendProgressMessages(newHookEvent(info, r), reader)
			defer close(stopProgressEvents)
		}

		bytesWritten, err = upload.WriteChunk(ctx, offset, reader)
		if terminateUpload && u.composer.UsesTerminater {
			if terminateErr := u.terminateUpload(ctx, upload, info, r); terminateErr != nil {
				// We only log this error and not show it to the user since this
				// termination error is not relevant to the uploading client
				u.log(ctx, "UploadStopTerminateError", "id", id, "error", terminateErr.Error())
			}
		}

		// If we encountered an error while reading the body from the HTTP request, log it, but only include
		// it in the response if the store did not also return an error.
		if bodyErr := reader.hasError(); bodyErr != nil {
			u.log(ctx, "BodyReadError", "id", id, "error", bodyErr.Error())
			if err == nil {
				err = bodyErr
			}
		}

		// If the upload was stopped by the server, send an error response indicating this.
		// TODO: Include a custom reason for the end user why the upload was stopped.
		if terminateUpload {
			err = ErrUploadStoppedByServer
		}
	}

	u.log(ctx, "ChunkWriteComplete", "id", id, "bytesWritten", i64toa(bytesWritten))

	if err != nil {
		return err
	}

	// Send new offset to a client
	newOffset := offset + bytesWritten
	r.Response.Header().Set("Upload-Offset", strconv.FormatInt(newOffset, 10))
	u.Metrics.incBytesReceived(uint64(bytesWritten))
	info.Offset = newOffset

	return u.finishUploadIfComplete(ctx, upload, info, r)
}

// finishUploadIfComplete checks whether an upload is completed (i.e., upload offset
// matches upload size) and if so, it will call the data store's FinishUpload
// function and send the necessary message on the CompleteUpload channel.
func (u *Uploader) finishUploadIfComplete(ctx context.Context, upload Upload, info FileInfo, r *ghttp.Request) error {
	// If the upload is completed, ...
	if !info.SizeIsDeferred && info.Offset == info.Size {
		// ... allow the data storage to finish and cleanup the upload
		if err := upload.FinishUpload(ctx); err != nil {
			return err
		}

		// ... allow the hook callback to run before sending the response
		if u.config.PreFinishResponseCallback != nil {
			if err := u.config.PreFinishResponseCallback(newHookEvent(info, r)); err != nil {
				return err
			}
		}

		u.Metrics.incUploadsFinished()

		// ... send the info out to the channel
		if u.config.NotifyCompleteUploads {
			u.CompleteUploads <- newHookEvent(info, r)
		}
	}

	return nil
}

// sendProgressMessage will send a notification over the UploadProgress channel
// every second, indicating how much data has been transferred to the server.
// It will stop sending these instances once the returned channel has been
// closed.
func (u *Uploader) sendProgressMessages(hook HookEvent, reader *bodyReader) chan<- struct{} {
	previousOffset := int64(0)
	originalOffset := hook.Upload.Offset
	stop := make(chan struct{}, 1)

	go func() {
		for {
			select {
			case <-stop:
				hook.Upload.Offset = originalOffset + reader.bytesRead()
				if hook.Upload.Offset != previousOffset {
					u.UploadProgress <- hook
					previousOffset = hook.Upload.Offset
				}
				return
			case <-time.After(1 * time.Second):
				hook.Upload.Offset = originalOffset + reader.bytesRead()
				if hook.Upload.Offset != previousOffset {
					u.UploadProgress <- hook
					previousOffset = hook.Upload.Offset
				}
			}
		}
	}()

	return stop
}

// terminateUpload passes a given upload to the DataStore's Terminater,
// send the corresponding upload info on the TerminatedUploads channnel,
// and updates the statistics.
// Note the the info argument is only needed if the terminated uploads
// notifications are enabled.
func (u *Uploader) terminateUpload(ctx context.Context, upload Upload, info FileInfo, r *ghttp.Request) error {
	terminatableUpload := u.composer.Terminater.AsTerminatableUpload(upload)

	err := terminatableUpload.Terminate(ctx)
	if err != nil {
		return err
	}

	if u.config.NotifyTerminatedUploads {
		u.TerminatedUploads <- newHookEvent(info, r)
	}

	u.Metrics.incUploadsTerminated()

	return nil
}

// The get sum of all sizes for a list of upload ids while checking whether
// all of these uploads are already finished.
// This is used to calculate the size
// of a final resource.
func (u *Uploader) sizeOfUploads(ctx context.Context, ids []string) (partialUploads []Upload, size int64, err error) {
	partialUploads = make([]Upload, len(ids))

	for i, id := range ids {
		upload, err := u.composer.Core.GetUpload(ctx, id)
		if err != nil {
			return nil, 0, err
		}

		info, err := upload.GetInfo(ctx)
		if err != nil {
			return nil, 0, err
		}

		if info.SizeIsDeferred || info.Offset != info.Size {
			err = ErrUploadNotFinished
			return nil, 0, err
		}

		size += info.Size
		partialUploads[i] = upload
	}

	return
}

// Verify that the Upload-Length and Upload-Defer-Length headers are acceptable for creating a
// new upload
func (u *Uploader) validateNewUploadLengthHeaders(uploadLengthHeader string, uploadDeferLengthHeader string) (uploadLength int64, uploadLengthDeferred bool, err error) {
	haveBothLengthHeaders := uploadLengthHeader != "" && uploadDeferLengthHeader != ""
	haveInvalidDeferHeader := uploadDeferLengthHeader != "" && uploadDeferLengthHeader != UploadLengthDeferred
	lengthIsDeferred := uploadDeferLengthHeader == UploadLengthDeferred

	if lengthIsDeferred && !u.composer.UsesLengthDeferrer {
		err = ErrNotImplemented
	} else if haveBothLengthHeaders {
		err = ErrUploadLengthAndUploadDeferLength
	} else if haveInvalidDeferHeader {
		err = ErrInvalidUploadDeferLength
	} else if lengthIsDeferred {
		uploadLengthDeferred = true
	} else {
		uploadLength, err = strconv.ParseInt(uploadLengthHeader, 10, 64)
		if err != nil || uploadLength < 0 {
			err = ErrInvalidUploadLength
		}
	}

	return
}

// lockUpload creates a new lock for the given upload ID and attempts to lock it.
// The created lock is returned if it was aquired successfully.
func (u *Uploader) lockUpload(id string) (Lock, error) {
	lock, err := u.composer.Locker.NewLock(id)
	if err != nil {
		return nil, err
	}

	if err := lock.Lock(); err != nil {
		return nil, err
	}

	return lock, nil
}

// Make an absolute URLs to the given upload id. If the base path is absolute
// it will be prepended else the host and protocol from the request is used.
func (u *Uploader) absFileURL(r *ghttp.Request, id string) string {
	if u.isBasePathAbs {
		return u.basePath + id
	}

	// Read origin and protocol from request
	host, proto := getHostAndProtocol(r, u.config.RespectForwardedHeaders)

	url := proto + "://" + host + u.basePath + id

	return url
}
