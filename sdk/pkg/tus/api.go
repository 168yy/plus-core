package tus

import (
	"context"
	"io"
	"net/http"
	"strconv"
)

// PostFile creates a new file upload using the datastore after validating the
// length and parsing the metadata.
func (h *Uploader) PostFile(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	// Check for presence of application/offset+octet-stream. If another content
	// type is defined, it will be ignored and treated as none was set because
	// some HTTP clients may enforce a default value for this header.
	containsChunk := r.Header.Get("Content-Type") == "application/offset+octet-stream"

	// Only use the proper Upload-Concat header if the concatenation extension
	// is even supported by the data store.
	var concatHeader string
	if h.composer.UsesConcater {
		concatHeader = r.Header.Get("Upload-Concat")
	}

	// Parse Upload-Concat header
	isPartial, isFinal, partialUploadIDs, err := parseConcat(concatHeader)
	if err != nil {
		h.sendError(ctx, w, r, err)
		return
	}

	// If the upload is a final upload created by concatenation multiple partial
	// uploads the size is sum of all sizes of these files (no need for
	// Upload-Length header)
	var size int64
	var sizeIsDeferred bool
	var partialUploads []Upload
	if isFinal {
		// A final upload must not contain a chunk within the creation request
		if containsChunk {
			h.sendError(ctx, w, r, ErrModifyFinal)
			return
		}

		partialUploads, size, err = h.sizeOfUploads(ctx, partialUploadIDs)
		if err != nil {
			h.sendError(ctx, w, r, err)
			return
		}
	} else {
		uploadLengthHeader := r.Header.Get("Upload-Length")
		uploadDeferLengthHeader := r.Header.Get("Upload-Defer-Length")
		size, sizeIsDeferred, err = h.validateNewUploadLengthHeaders(uploadLengthHeader, uploadDeferLengthHeader)
		if err != nil {
			h.sendError(ctx, w, r, err)
			return
		}
	}

	// Test whether the size is still allowed
	if h.config.MaxSize > 0 && size > h.config.MaxSize {
		h.sendError(ctx, w, r, ErrMaxSizeExceeded)
		return
	}

	// Parse metadata
	meta := ParseMetadataHeader(r.Header.Get("Upload-Metadata"))

	info := FileInfo{
		Size:           size,
		SizeIsDeferred: sizeIsDeferred,
		MetaData:       meta,
		IsPartial:      isPartial,
		IsFinal:        isFinal,
		PartialUploads: partialUploadIDs,
	}

	if h.config.PreUploadCreateCallback != nil {
		if err := h.config.PreUploadCreateCallback(newHookEvent(info, r)); err != nil {
			h.sendError(ctx, w, r, err)
			return
		}
	}

	upload, err := h.composer.Core.NewUpload(ctx, info)
	if err != nil {
		h.sendError(ctx, w, r, err)
		return
	}

	info, err = upload.GetInfo(ctx)
	if err != nil {
		h.sendError(ctx, w, r, err)
		return
	}

	id := info.ID

	// Add the Location header directly after creating the new resource to even
	// include it in cases of failure when an error is returned
	url := h.absFileURL(r, id)
	w.Header().Set("Location", url)

	h.Metrics.incUploadsCreated()
	h.log(ctx, "UploadCreated", "id", id, "size", i64toa(size), "url", url)

	if h.config.NotifyCreatedUploads {
		h.CreatedUploads <- newHookEvent(info, r)
	}

	if isFinal {
		concatableUpload := h.composer.Concater.AsConcatableUpload(upload)
		if err := concatableUpload.ConcatUploads(ctx, partialUploads); err != nil {
			h.sendError(ctx, w, r, err)
			return
		}
		info.Offset = size

		if h.config.NotifyCompleteUploads {
			h.CompleteUploads <- newHookEvent(info, r)
		}
	}

	if containsChunk {
		if h.composer.UsesLocker {
			lock, err := h.lockUpload(id)
			if err != nil {
				h.sendError(ctx, w, r, err)
				return
			}

			defer func(lock Lock) {
				_ = lock.Unlock()
			}(lock)
		}

		if err := h.writeChunk(ctx, upload, info, w, r); err != nil {
			h.sendError(ctx, w, r, err)
			return
		}
	} else if !sizeIsDeferred && size == 0 {
		// Directly finish the upload if the upload is empty (i.e. has a size of 0).
		// This statement is in an else-if block to avoid causing duplicate calls
		// to finishUploadIfComplete if an upload is empty and contains a chunk.
		if err := h.finishUploadIfComplete(ctx, upload, info, r); err != nil {
			h.sendError(ctx, w, r, err)
			return
		}
	}

	h.sendResp(ctx, w, r, http.StatusCreated)
}

// HeadFile returns the length and offset for the HEAD request
func (h *Uploader) HeadFile(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	id, err := extractIDFromPath(r.URL.Path)
	if err != nil {
		h.sendError(ctx, w, r, err)
		return
	}

	if h.composer.UsesLocker {
		lock, err := h.lockUpload(id)
		if err != nil {
			h.sendError(ctx, w, r, err)
			return
		}

		defer func(lock Lock) {
			_ = lock.Unlock()
		}(lock)
	}

	upload, err := h.composer.Core.GetUpload(ctx, id)
	if err != nil {
		h.sendError(ctx, w, r, err)
		return
	}

	info, err := upload.GetInfo(ctx)
	if err != nil {
		h.sendError(ctx, w, r, err)
		return
	}

	// Add Upload-Concat header if possible
	if info.IsPartial {
		w.Header().Set("Upload-Concat", "partial")
	}

	if info.IsFinal {
		v := "final;"
		for _, uploadID := range info.PartialUploads {
			v += h.absFileURL(r, uploadID) + " "
		}
		// Remove trailing space
		v = v[:len(v)-1]

		w.Header().Set("Upload-Concat", v)
	}

	if len(info.MetaData) != 0 {
		w.Header().Set("Upload-Metadata", SerializeMetadataHeader(info.MetaData))
	}

	if info.SizeIsDeferred {
		w.Header().Set("Upload-Defer-Length", UploadLengthDeferred)
	} else {
		w.Header().Set("Upload-Length", strconv.FormatInt(info.Size, 10))
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Upload-Offset", strconv.FormatInt(info.Offset, 10))
	h.sendResp(ctx, w, r, http.StatusOK)
}

// PatchFile adds a chunk to an upload. This operation is only allowed
// if enough space in the upload is left.
func (h *Uploader) PatchFile(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	// Check for presence of application/offset+octet-stream
	if r.Header.Get("Content-Type") != "application/offset+octet-stream" {
		h.sendError(ctx, w, r, ErrInvalidContentType)
		return
	}

	// Check for presence of a valid Upload-Offset Header
	offset, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil || offset < 0 {
		h.sendError(ctx, w, r, ErrInvalidOffset)
		return
	}

	id, err := extractIDFromPath(r.URL.Path)
	if err != nil {
		h.sendError(ctx, w, r, err)
		return
	}

	if h.composer.UsesLocker {
		lock, err := h.lockUpload(id)
		if err != nil {
			h.sendError(ctx, w, r, err)
			return
		}

		defer func(lock Lock) {
			_ = lock.Unlock()
		}(lock)
	}

	upload, err := h.composer.Core.GetUpload(ctx, id)
	if err != nil {
		h.sendError(ctx, w, r, err)
		return
	}

	info, err := upload.GetInfo(ctx)
	if err != nil {
		h.sendError(ctx, w, r, err)
		return
	}

	// Modifying a final upload is not allowed
	if info.IsFinal {
		h.sendError(ctx, w, r, ErrModifyFinal)
		return
	}

	if offset != info.Offset {
		h.sendError(ctx, w, r, ErrMismatchOffset)
		return
	}

	// Do not proxy the call to the data store if the upload is already completed
	if !info.SizeIsDeferred && info.Offset == info.Size {
		w.Header().Set("Upload-Offset", strconv.FormatInt(offset, 10))
		h.sendResp(ctx, w, r, http.StatusNoContent)
		return
	}

	if r.Header.Get("Upload-Length") != "" {
		if !h.composer.UsesLengthDeferrer {
			h.sendError(ctx, w, r, ErrNotImplemented)
			return
		}
		if !info.SizeIsDeferred {
			h.sendError(ctx, w, r, ErrInvalidUploadLength)
			return
		}
		uploadLength, err := strconv.ParseInt(r.Header.Get("Upload-Length"), 10, 64)
		if err != nil || uploadLength < 0 || uploadLength < info.Offset || (h.config.MaxSize > 0 && uploadLength > h.config.MaxSize) {
			h.sendError(ctx, w, r, ErrInvalidUploadLength)
			return
		}

		lengthDeclarableUpload := h.composer.LengthDeferrer.AsLengthDeclarableUpload(upload)
		if err := lengthDeclarableUpload.DeclareLength(ctx, uploadLength); err != nil {
			h.sendError(ctx, w, r, err)
			return
		}

		info.Size = uploadLength
		info.SizeIsDeferred = false

	}

	if err := h.writeChunk(ctx, upload, info, w, r); err != nil {
		h.sendError(ctx, w, r, err)
		return
	}

	h.sendResp(ctx, w, r, http.StatusNoContent)
}

// GetFile handles requests to download a file using a GET request. This is not
// part of the specification.
func (h *Uploader) GetFile(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	id, err := extractIDFromPath(r.URL.Path)
	if err != nil {
		h.sendError(ctx, w, r, err)
		return
	}

	if h.composer.UsesLocker {
		lock, err := h.lockUpload(id)
		if err != nil {
			h.sendError(ctx, w, r, err)
			return
		}

		defer func(lock Lock) {
			_ = lock.Unlock()
		}(lock)
	}

	upload, err := h.composer.Core.GetUpload(ctx, id)
	if err != nil {
		h.sendError(ctx, w, r, err)
		return
	}

	info, err := upload.GetInfo(ctx)
	if err != nil {
		h.sendError(ctx, w, r, err)
		return
	}

	// Set headers before sending responses
	w.Header().Set("Content-Length", strconv.FormatInt(info.Offset, 10))

	contentType, contentDisposition := filterContentType(info)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", contentDisposition)

	// If no data has been uploaded yet, respond with an empty "204 No Content" status.
	if info.Offset == 0 {
		h.sendResp(ctx, w, r, http.StatusNoContent)
		return
	}

	src, err := upload.GetReader(ctx)
	if err != nil {
		h.sendError(ctx, w, r, err)
		return
	}

	h.sendResp(ctx, w, r, http.StatusOK)
	_, _ = io.Copy(w, src)

	// Try to close the reader if the io.Closer interface is implemented
	if closer, ok := src.(io.Closer); ok {
		_ = closer.Close()
	}
}

// DelFile terminates an upload permanently.
func (h *Uploader) DelFile(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	// Abort the request handling if the required interface is not implemented
	if !h.composer.UsesTerminater {
		h.sendError(ctx, w, r, ErrNotImplemented)
		return
	}

	id, err := extractIDFromPath(r.URL.Path)
	if err != nil {
		h.sendError(ctx, w, r, err)
		return
	}

	if h.composer.UsesLocker {
		lock, err := h.lockUpload(id)
		if err != nil {
			h.sendError(ctx, w, r, err)
			return
		}

		defer func(lock Lock) {
			_ = lock.Unlock()
		}(lock)
	}

	upload, err := h.composer.Core.GetUpload(ctx, id)
	if err != nil {
		h.sendError(ctx, w, r, err)
		return
	}

	var info FileInfo
	if h.config.NotifyTerminatedUploads {
		info, err = upload.GetInfo(ctx)
		if err != nil {
			h.sendError(ctx, w, r, err)
			return
		}
	}

	err = h.terminateUpload(ctx, upload, info, r)
	if err != nil {
		h.sendError(ctx, w, r, err)
		return
	}

	h.sendResp(ctx, w, r, http.StatusNoContent)
}
