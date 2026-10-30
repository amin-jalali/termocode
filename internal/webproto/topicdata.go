package webproto

import (
	"reflect"
	"sort"
)

// The data of topics and events. A snap's and a delta's "data" (and an
// ev's "data") are hand-written structs registered here per topic: the
// hub marshals exactly these types, and the generator writes them into
// proto.d.ts (TopicData, EventData) and schema.json, so the browser store
// knows each shape and can check it before it applies a delta.
//
// Shape rules every topic follows:
//   - a delta is one object; when a topic has several kinds of change its
//     "op" says which one, and the other fields are set for that op only;
//   - lists are replaced or edited by id, never by position, except where
//     a field says otherwise (chat messages carry their index "i");
//   - a string field with an `enum:"a|b"` tag only ever holds those values.

// TopicData names the types of one topic's snap and delta data.
type TopicData struct {
	Topic string
	Snap  reflect.Type
	Delta reflect.Type
}

var (
	topicData  = map[string]TopicData{}
	eventData  = map[string]reflect.Type{}
	resultData = map[string]reflect.Type{}
)

// registerTopic records the data types of a known topic: zero values of
// the snapshot and delta structs. A second registration or an unknown
// topic is a programming error.
func registerTopic(topic string, snap, delta any) {
	if _, ok := topicDef(topic); !ok {
		panic("webproto: data for unknown topic " + topic)
	}
	if _, dup := topicData[topic]; dup {
		panic("webproto: topic data registered twice: " + topic)
	}
	topicData[topic] = TopicData{Topic: topic, Snap: structType(snap), Delta: structType(delta)}
}

// registerEvent records the data type of one ev kind.
func registerEvent(kind string, data any) {
	if _, dup := eventData[kind]; dup {
		panic("webproto: event registered twice: " + kind)
	}
	eventData[kind] = structType(data)
}

// registerResult records the data type of an op's res.
func registerResult(op string, data any) {
	if _, ok := Lookup(op); !ok {
		panic("webproto: result for unknown op " + op)
	}
	if _, dup := resultData[op]; dup {
		panic("webproto: result registered twice: " + op)
	}
	resultData[op] = structType(data)
}

// ResultOf returns the data type of an op's res (false: the res has no
// data).
func ResultOf(op string) (reflect.Type, bool) {
	t, ok := resultData[op]
	return t, ok
}

func structType(v any) reflect.Type {
	t := reflect.TypeOf(v)
	if t == nil || t.Kind() != reflect.Struct {
		panic("webproto: topic and event data must be structs")
	}
	return t
}

// TopicDataOf returns the registered data types of a topic.
func TopicDataOf(topic string) (TopicData, bool) {
	d, ok := topicData[topic]
	return d, ok
}

// topicDataList is the registered topic data in topic table order.
func topicDataList() []TopicData {
	var out []TopicData
	for _, d := range topicDefs {
		if td, ok := topicData[d.Name]; ok {
			out = append(out, td)
		}
	}
	return out
}

// EventKinds lists the ev kinds with registered data, sorted.
func EventKinds() []string {
	out := make([]string, 0, len(eventData))
	for k := range eventData {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// EventDataOf returns the data type of an ev kind.
func EventDataOf(kind string) (reflect.Type, bool) {
	t, ok := eventData[kind]
	return t, ok
}
