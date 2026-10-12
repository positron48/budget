package grpcadapter

import (
	"bytes"
	"testing"

	pb "github.com/positron48/budget/gen/go/budget/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Exercise old client envelopes against the dedicated RPC envelopes. Matching
// names alone would not detect lost field numbers, precision or nested timing.
func TestInvestmentRPCEnvelopeWireCompatibility(t *testing.T) {
	cases := []struct{ old, current proto.Message }{
		{&pb.MutateInvestmentTransferRequest{}, &pb.PreviewInvestmentTransferRequest{}},
		{&pb.MutateInvestmentCashTransferRequest{}, &pb.PreviewInvestmentCashTransferRequest{}},
		{&pb.GetInvestmentNAVSeriesRequest{}, &pb.GetInvestmentCurrentNAVRequest{}},
		{&pb.GetInvestmentPerformanceRequest{}, &pb.ExportInvestmentSeriesRequest{}},
		{&pb.GetInvestmentPerformanceRequest{}, &pb.GetInvestmentOverviewRequest{}},
		{&pb.GetInvestmentPerformanceRequest{}, &pb.GetInvestmentProfitRequest{}},
		{&pb.InvestmentNAVVariantRequest{}, &pb.GetInvestmentNAVVariantRequest{}},
		{&pb.InvestmentNAVVariantRequest{}, &pb.PrepareInvestmentNAVVariantRequest{}},
		{&pb.InvestmentNAVVariantResponse{}, &pb.GetInvestmentNAVVariantResponse{}},
		{&pb.InvestmentNAVVariantResponse{}, &pb.PrepareInvestmentNAVVariantResponse{}},
	}
	for _, c := range cases {
		t.Run(string(c.current.ProtoReflect().Descriptor().Name()), func(t *testing.T) {
			oldFields, newFields := c.old.ProtoReflect().Descriptor().Fields(), c.current.ProtoReflect().Descriptor().Fields()
			if oldFields.Len() != newFields.Len() {
				t.Fatal("field lost or introduced without compatibility review")
			}
			for i := 0; i < oldFields.Len(); i++ {
				a := oldFields.Get(i)
				b := newFields.ByNumber(a.Number())
				if b == nil || a.Kind() != b.Kind() || a.JSONName() != b.JSONName() || a.Cardinality() != b.Cardinality() || a.HasPresence() != b.HasPresence() {
					t.Fatal("field contract changed", a.FullName())
				}
			}
			fillEnvelopeFixture(t, c.old.ProtoReflect())
			wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(c.old)
			if err != nil {
				t.Fatal(err)
			}
			if err = proto.Unmarshal(wire, c.current); err != nil {
				t.Fatal(err)
			}
			newWire, err := proto.MarshalOptions{Deterministic: true}.Marshal(c.current)
			if err != nil || !bytes.Equal(wire, newWire) {
				t.Fatal("binary payload changed", err)
			}
			oldJSON, err := protojson.Marshal(c.old)
			if err != nil {
				t.Fatal(err)
			}
			newJSON, err := protojson.Marshal(c.current)
			if err != nil || !bytes.Equal(oldJSON, newJSON) {
				t.Fatal("JSON payload changed", string(oldJSON), string(newJSON), err)
			}
			// Unknown future fields survive a binary round trip through either type.
			unknown := protowire.AppendTag(nil, 511, protowire.VarintType)
			unknown = protowire.AppendVarint(unknown, 9007199254740993)
			wire = append(wire, unknown...)
			if err = proto.Unmarshal(wire, c.current); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(c.current.ProtoReflect().GetUnknown(), unknown) {
				t.Fatal("unknown field lost")
			}
			newWire, err = proto.MarshalOptions{Deterministic: true}.Marshal(c.current)
			if err != nil || !bytes.Equal(wire, newWire) {
				t.Fatal("unknown field round trip changed", err)
			}
		})
	}
}

func fillEnvelopeFixture(t *testing.T, m protoreflect.Message) {
	t.Helper()
	if m.Descriptor().FullName() == "google.protobuf.Timestamp" {
		m.Set(m.Descriptor().Fields().ByName("seconds"), protoreflect.ValueOfInt64(1700000000))
		m.Set(m.Descriptor().Fields().ByName("nanos"), protoreflect.ValueOfInt32(123456789))
		return
	}
	fields := m.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		if f.IsMap() {
			t.Fatal("add map fixture support for new contract", f.FullName())
		}
		if f.IsList() {
			list := m.Mutable(f).List()
			if f.Kind() == protoreflect.MessageKind {
				v := list.NewElement()
				fillEnvelopeFixture(t, v.Message())
				list.Append(v)
			} else {
				list.Append(envelopeScalar(t, f))
			}
		} else if f.Kind() == protoreflect.MessageKind {
			fillEnvelopeFixture(t, m.Mutable(f).Message())
		} else {
			m.Set(f, envelopeScalar(t, f))
		}
	}
}
func envelopeScalar(t *testing.T, f protoreflect.FieldDescriptor) protoreflect.Value {
	t.Helper()
	switch f.Kind() {
	case protoreflect.StringKind:
		return protoreflect.ValueOfString("Пример:" + string(f.Name()))
	case protoreflect.BoolKind:
		return protoreflect.ValueOfBool(true)
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return protoreflect.ValueOfInt64(9007199254740993 + int64(f.Number()))
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return protoreflect.ValueOfInt32(7 + int32(f.Number()))
	default:
		t.Fatalf("add scalar fixture support for %s", f.FullName())
		return protoreflect.Value{}
	}
}
