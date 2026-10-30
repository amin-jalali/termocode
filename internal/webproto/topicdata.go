package webproto

import (
	"reflect"
	"sort"
	"strings"
)

// The data of topics and events. A snap's and a delta's "data" (and an
// ev's "data") are hand-written structs registered here per topic: the
// hub marshals exactly these types, and the generator writes them into
// proto.d.ts (TopicData, EventData) and schema.json, so the browser store
// knows each shape and can check it before it applies a delta.
//
// A view file registers its types from an init(); the generator orders
// them by the topic and op tables.
//
// Shape rules every topic follows:
//   - a delta is one object. When a topic has several kinds of change,
//     either one delta struct carries an "op" with an enum tag and the
//     other fields are set for that op only, or the topic registers one
//     struct per change, each with a Kind field tagged `const:"<kind>"`
//     (TypeScript then sees a literal type and can narrow on it);
//   - lists are replaced or edited by id, never by position, except where
//     a field says otherwise (chat messages carry their index "i");
//   - a string field with an `enum:"a|b"` tag only ever holds those values.

// TopicData names the types of one topic's snap and delta data: a delta
// is one of Deltas.
type TopicData struct {
	Topic  string
	Snap   reflect.Type
	Deltas []reflect.Type
}

// deltaNames is the delta type names, in registration order, joined.
func (d TopicData) deltaNames(sep string) string {
	names := make([]string, len(d.Deltas))
	for i, t := range d.Deltas {
		names[i] = t.Name()
	}
	return strings.Join(names, sep)
}

var (
	topicData  = map[string]TopicData{}
	eventData  = map[string]reflect.Type{}
	resultData = map[string]reflect.Type{}
)

// registerTopic records the data types of a known topic (init only):
// zero values of the snapshot struct and of each delta struct. A second
// registration, an unknown topic or no delta is a programming error.
func registerTopic(topic string, snap any, deltas ...any) {
	if _, ok := topicDef(topic); !ok {
		panic("webproto: data for unknown topic " + topic)
	}
	if len(deltas) == 0 {
		panic("webproto: topic without a delta type: " + topic)
	}
	if _, dup := topicData[topic]; dup {
		panic("webproto: topic data registered twice: " + topic)
	}
	d := TopicData{Topic: topic, Snap: structType(snap)}
	for _, x := range deltas {
		d.Deltas = append(d.Deltas, structType(x))
	}
	topicData[topic] = d
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
