package otlp

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/frei-l/trae/internal/model"
)

// OTLP/JSON differs from protojson: ids are hex, 64-bit integers may be
// strings, and enums may be numbers or names. These types accept all of
// those spellings.

type jsonRequest struct {
	ResourceSpans []struct {
		Resource struct {
			Attributes []jsonKV `json:"attributes"`
		} `json:"resource"`
		ScopeSpans []struct {
			Scope struct {
				Name string `json:"name"`
			} `json:"scope"`
			Spans []jsonSpan `json:"spans"`
		} `json:"scopeSpans"`
	} `json:"resourceSpans"`
}

type jsonSpan struct {
	TraceID      string   `json:"traceId"`
	SpanID       string   `json:"spanId"`
	ParentSpanID string   `json:"parentSpanId"`
	Name         string   `json:"name"`
	Kind         flexEnum `json:"kind"`
	Start        flexInt  `json:"startTimeUnixNano"`
	End          flexInt  `json:"endTimeUnixNano"`
	Attributes   []jsonKV `json:"attributes"`
	Events       []struct {
		Time       flexInt  `json:"timeUnixNano"`
		Name       string   `json:"name"`
		Attributes []jsonKV `json:"attributes"`
	} `json:"events"`
	Links []struct {
		TraceID    string   `json:"traceId"`
		SpanID     string   `json:"spanId"`
		Attributes []jsonKV `json:"attributes"`
	} `json:"links"`
	Status struct {
		Code    flexEnum `json:"code"`
		Message string   `json:"message"`
	} `json:"status"`
}

type jsonKV struct {
	Key   string    `json:"key"`
	Value jsonValue `json:"value"`
}

type jsonValue struct {
	StringValue *string  `json:"stringValue"`
	BoolValue   *bool    `json:"boolValue"`
	IntValue    *flexInt `json:"intValue"`
	DoubleValue *float64 `json:"doubleValue"`
	BytesValue  *string  `json:"bytesValue"`
	ArrayValue  *struct {
		Values []jsonValue `json:"values"`
	} `json:"arrayValue"`
	KvlistValue *struct {
		Values []jsonKV `json:"values"`
	} `json:"kvlistValue"`
}

// flexInt is an int64 written as a number or a string.
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		fl, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil {
			return err
		}
		n = int64(fl)
	}
	*f = flexInt(n)
	return nil
}

// flexEnum is an enum written as a number or as its proto name, such as
// "SPAN_KIND_SERVER" or "STATUS_CODE_ERROR".
type flexEnum int

var enumNames = map[string]int{
	"SPAN_KIND_UNSPECIFIED": 0, "SPAN_KIND_INTERNAL": 1, "SPAN_KIND_SERVER": 2,
	"SPAN_KIND_CLIENT": 3, "SPAN_KIND_PRODUCER": 4, "SPAN_KIND_CONSUMER": 5,
	"STATUS_CODE_UNSET": 0, "STATUS_CODE_OK": 1, "STATUS_CODE_ERROR": 2,
}

func (f *flexEnum) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexEnum(enumNames[s])
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = flexEnum(n)
	return nil
}

func decodeJSON(body []byte) ([]model.Span, error) {
	var req jsonRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&req); err != nil {
		return nil, err
	}
	var out []model.Span
	for _, rs := range req.ResourceSpans {
		resource := jsonAttrs(rs.Resource.Attributes)
		for _, ss := range rs.ScopeSpans {
			for _, s := range ss.Spans {
				span := model.Span{
					TraceID:    normID(s.TraceID),
					SpanID:     normID(s.SpanID),
					ParentID:   normID(s.ParentSpanID),
					Name:       s.Name,
					Kind:       spanKind(int(s.Kind)),
					Scope:      ss.Scope.Name,
					StartNs:    int64(s.Start),
					EndNs:      int64(s.End),
					StatusCode: statusCode(int(s.Status.Code)),
					StatusMsg:  s.Status.Message,
					Attrs:      jsonAttrs(s.Attributes),
					Resource:   resource,
				}
				for _, e := range s.Events {
					span.Events = append(span.Events, model.Event{Name: e.Name, TimeNs: int64(e.Time), Attrs: jsonAttrs(e.Attributes)})
				}
				for _, l := range s.Links {
					span.Links = append(span.Links, model.Link{TraceID: normID(l.TraceID), SpanID: normID(l.SpanID), Attrs: jsonAttrs(l.Attributes)})
				}
				out = append(out, span)
			}
		}
	}
	return out, nil
}

// normID returns a lowercase hex id. Ids are hex in OTLP/JSON, but some
// senders use protojson's base64 for bytes; those are converted.
func normID(s string) string {
	if s == "" {
		return ""
	}
	if _, err := hex.DecodeString(s); err == nil {
		return strings.ToLower(s)
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return hex.EncodeToString(b)
	}
	return strings.ToLower(s)
}

func jsonAttrs(kvs []jsonKV) map[string]any {
	m := make(map[string]any, len(kvs))
	for _, kv := range kvs {
		m[kv.Key] = kv.Value.value()
	}
	return m
}

func (v jsonValue) value() any {
	switch {
	case v.StringValue != nil:
		return *v.StringValue
	case v.BoolValue != nil:
		return *v.BoolValue
	case v.IntValue != nil:
		return int64(*v.IntValue)
	case v.DoubleValue != nil:
		return *v.DoubleValue
	case v.BytesValue != nil:
		return *v.BytesValue
	case v.ArrayValue != nil:
		arr := make([]any, 0, len(v.ArrayValue.Values))
		for _, e := range v.ArrayValue.Values {
			arr = append(arr, e.value())
		}
		return arr
	case v.KvlistValue != nil:
		return jsonAttrs(v.KvlistValue.Values)
	}
	return nil
}
