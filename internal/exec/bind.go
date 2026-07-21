package exec

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

var tokenRe = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

// BindSQL resolve tokens {campo} do template contra os campos da request.
// O cliente nunca envia SQL: só valores para slots tipados. Strings são
// escapadas ('' ) e validadas contra caracteres de controle; repeated vira
// lista para IN (...). Isso NÃO é prepared statement (a REST API do broker
// não tem bind server-side) — a segurança vem do template fixo + tipagem.
func BindSQL(template string, req protoreflect.Message) (string, error) {
	var bindErr error
	sql := tokenRe.ReplaceAllStringFunc(template, func(tok string) string {
		if bindErr != nil {
			return tok
		}
		name := tok[1 : len(tok)-1]
		fd := req.Descriptor().Fields().ByName(protoreflect.Name(name))
		if fd == nil {
			bindErr = fmt.Errorf("token {%s} não existe na request %s", name, req.Descriptor().FullName())
			return tok
		}
		rendered, err := renderField(fd, req.Get(fd))
		if err != nil {
			bindErr = fmt.Errorf("token {%s}: %w", name, err)
			return tok
		}
		return rendered
	})
	return sql, bindErr
}

func renderField(fd protoreflect.FieldDescriptor, v protoreflect.Value) (string, error) {
	if fd.IsList() {
		list := v.List()
		if list.Len() == 0 {
			return "", fmt.Errorf("lista vazia (IN () é SQL inválido)")
		}
		parts := make([]string, 0, list.Len())
		for i := 0; i < list.Len(); i++ {
			s, err := renderScalar(fd, list.Get(i))
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, ", "), nil
	}
	return renderScalar(fd, v)
}

func renderScalar(fd protoreflect.FieldDescriptor, v protoreflect.Value) (string, error) {
	switch fd.Kind() {
	case protoreflect.Int32Kind, protoreflect.Int64Kind,
		protoreflect.Sint32Kind, protoreflect.Sint64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Sfixed64Kind:
		return strconv.FormatInt(v.Int(), 10), nil
	case protoreflect.Uint32Kind, protoreflect.Uint64Kind,
		protoreflect.Fixed32Kind, protoreflect.Fixed64Kind:
		return strconv.FormatUint(v.Uint(), 10), nil
	case protoreflect.DoubleKind, protoreflect.FloatKind:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64), nil
	case protoreflect.BoolKind:
		return strconv.FormatBool(v.Bool()), nil
	case protoreflect.StringKind:
		s := v.String()
		for _, r := range s {
			if r < 0x20 || r == 0x7f {
				return "", fmt.Errorf("string contém caractere de controle")
			}
		}
		return "'" + strings.ReplaceAll(s, "'", "''") + "'", nil
	default:
		return "", fmt.Errorf("kind %s não suportado como parâmetro", fd.Kind())
	}
}
