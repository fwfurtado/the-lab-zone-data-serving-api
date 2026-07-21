package debugx

import (
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func JSON(m protoreflect.Message) string {
	marshaller := protojson.MarshalOptions{
		Multiline:       false,
		EmitUnpopulated: true,
	}
	return marshaller.Format(m.Interface())
}

func PrettyJSON(m protoreflect.Message) string {
	marshaller := protojson.MarshalOptions{
		Multiline:       true,
		Indent:          "  ",
		EmitUnpopulated: true,
	}

	return marshaller.Format(m.Interface())
}

func Field(m protoreflect.Message, name string) string {
    fd := m.Descriptor().Fields().ByName(protoreflect.Name(name))
    if fd == nil {
        return "<campo inexistente>"
    }
    return m.Get(fd).String()
}
