package otlp

import (
	"encoding/base64"
	"encoding/hex"

	"github.com/frei-l/trae/internal/model"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// decodeProto reads an ExportTraceServiceRequest. Its wire format is the
// same as TracesData (field 1, repeated ResourceSpans), which keeps the
// gRPC service packages out of the build.
func decodeProto(body []byte) ([]model.Span, error) {
	var data tracepb.TracesData
	if err := proto.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	var out []model.Span
	for _, rs := range data.ResourceSpans {
		resource := protoAttrs(rs.GetResource().GetAttributes())
		for _, ss := range rs.ScopeSpans {
			scope := ss.GetScope().GetName()
			for _, s := range ss.Spans {
				span := model.Span{
					TraceID:    hex.EncodeToString(s.TraceId),
					SpanID:     hex.EncodeToString(s.SpanId),
					ParentID:   hex.EncodeToString(s.ParentSpanId),
					Name:       s.Name,
					Kind:       spanKind(int(s.Kind)),
					Scope:      scope,
					StartNs:    int64(s.StartTimeUnixNano),
					EndNs:      int64(s.EndTimeUnixNano),
					StatusCode: statusCode(int(s.GetStatus().GetCode())),
					StatusMsg:  s.GetStatus().GetMessage(),
					Attrs:      protoAttrs(s.Attributes),
					Resource:   resource,
				}
				for _, e := range s.Events {
					span.Events = append(span.Events, model.Event{Name: e.Name, TimeNs: int64(e.TimeUnixNano), Attrs: protoAttrs(e.Attributes)})
				}
				for _, l := range s.Links {
					span.Links = append(span.Links, model.Link{TraceID: hex.EncodeToString(l.TraceId), SpanID: hex.EncodeToString(l.SpanId), Attrs: protoAttrs(l.Attributes)})
				}
				out = append(out, span)
			}
		}
	}
	return out, nil
}

func protoAttrs(kvs []*commonpb.KeyValue) map[string]any {
	m := make(map[string]any, len(kvs))
	for _, kv := range kvs {
		m[kv.Key] = protoValue(kv.Value)
	}
	return m
}

func protoValue(v *commonpb.AnyValue) any {
	switch x := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return x.StringValue
	case *commonpb.AnyValue_BoolValue:
		return x.BoolValue
	case *commonpb.AnyValue_IntValue:
		return x.IntValue
	case *commonpb.AnyValue_DoubleValue:
		return x.DoubleValue
	case *commonpb.AnyValue_BytesValue:
		return base64.StdEncoding.EncodeToString(x.BytesValue)
	case *commonpb.AnyValue_ArrayValue:
		arr := make([]any, 0, len(x.ArrayValue.GetValues()))
		for _, e := range x.ArrayValue.GetValues() {
			arr = append(arr, protoValue(e))
		}
		return arr
	case *commonpb.AnyValue_KvlistValue:
		return protoAttrs(x.KvlistValue.GetValues())
	}
	return nil
}

var spanKinds = []string{"unspecified", "internal", "server", "client", "producer", "consumer"}

func spanKind(k int) string {
	if k > 0 && k < len(spanKinds) {
		return spanKinds[k]
	}
	return "internal"
}

func statusCode(c int) string {
	switch c {
	case 1:
		return "ok"
	case 2:
		return "error"
	}
	return "unset"
}
