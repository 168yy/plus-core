package message

import (
	"github.com/168yy/plus-core/core/v2/message"
)

type Message struct {
	Id         string
	RoutingKey string
	Values     map[string]interface{}
	GroupId    string
	ErrorCount uint64
}

func (m *Message) GetId() string {
	return m.Id
}

func (m *Message) GetRoutingKey() string {
	return m.RoutingKey
}

func (m *Message) GetValues() map[string]interface{} {
	return m.Values
}

func (m *Message) SetId(id string) {
	m.Id = id
}

func (m *Message) SetRoutingKey(routingKey string) {
	m.RoutingKey = routingKey
}

func (m *Message) SetValues(values map[string]interface{}) {
	m.Values = values
}

func (m *Message) GetPrefix() (prefix string) {
	if m.Values == nil {
		return
	}
	v, _ := m.Values[message.PrefixKey]
	prefix, _ = v.(string)
	return
}

func (m *Message) SetPrefix(prefix string) {
	if m.Values == nil {
		m.Values = make(map[string]interface{})
	}
	m.Values[message.PrefixKey] = prefix
}

func (m *Message) SetErrorIncr() {
	m.ErrorCount = m.ErrorCount + 1
}

func (m *Message) SetErrorCount(count uint64) {
	m.ErrorCount = count
}

func (m *Message) GetErrorCount() uint64 {
	return m.ErrorCount
}
