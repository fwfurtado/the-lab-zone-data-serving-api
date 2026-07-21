package exec

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// testMessage constrói dinamicamente um message descriptor de teste — o binder
// só enxerga protoreflect, então o teste não depende de código gerado.
func testMessage(t *testing.T) *dynamicpb.Message {
	t.Helper()
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("test.proto"),
		Package: proto.String("test"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Req"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{Name: proto.String("account_id"), Number: proto.Int32(1), Type: descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()},
				{Name: proto.String("regions"), Number: proto.Int32(2), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(), Label: descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()},
				{Name: proto.String("name"), Number: proto.Int32(3), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()},
			},
		}},
	}
	fd, err := protodesc.NewFile(fdp, nil)
	if err != nil {
		t.Fatalf("descriptor de teste: %v", err)
	}
	return dynamicpb.NewMessage(fd.Messages().Get(0))
}

func TestBindSQL_IntAndList(t *testing.T) {
	msg := testMessage(t)
	fields := msg.Descriptor().Fields()
	msg.Set(fields.ByName("account_id"), protoreflect.ValueOfInt64(42))
	list := msg.Mutable(fields.ByName("regions")).List()
	list.Append(protoreflect.ValueOfString("br-se"))
	list.Append(protoreflect.ValueOfString("br-s"))

	sql, err := BindSQL("SELECT * FROM t WHERE id = {account_id} AND region IN ({regions})", msg)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	want := "SELECT * FROM t WHERE id = 42 AND region IN ('br-se', 'br-s')"
	if sql != want {
		t.Fatalf("sql = %q, want %q", sql, want)
	}
}

func TestBindSQL_EscapesQuotes(t *testing.T) {
	msg := testMessage(t)
	msg.Set(msg.Descriptor().Fields().ByName("name"), protoreflect.ValueOfString("o'brien'; DROP TABLE accounts--"))

	sql, err := BindSQL("WHERE name = {name}", msg)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if !strings.Contains(sql, "'o''brien''; DROP TABLE accounts--'") {
		t.Fatalf("escape incorreto: %q", sql)
	}
}

func TestBindSQL_EmptyListFails(t *testing.T) {
	msg := testMessage(t)
	if _, err := BindSQL("IN ({regions})", msg); err == nil {
		t.Fatal("lista vazia deveria falhar")
	}
}

func TestBindSQL_UnknownTokenFails(t *testing.T) {
	msg := testMessage(t)
	if _, err := BindSQL("WHERE x = {nope}", msg); err == nil {
		t.Fatal("token inexistente deveria falhar")
	}
}

func TestBindSQL_ControlCharsFail(t *testing.T) {
	msg := testMessage(t)
	msg.Set(msg.Descriptor().Fields().ByName("name"), protoreflect.ValueOfString("a\nb"))
	if _, err := BindSQL("WHERE name = {name}", msg); err == nil {
		t.Fatal("caractere de controle deveria falhar")
	}
}
