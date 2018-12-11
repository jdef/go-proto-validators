// Copyright 2016 Michal Witkowski. All Rights Reserved.
// See LICENSE for licensing terms.

/*

The validator plugin generates a Validate method for each message.
By default, if none of the message's fields are annotated with the gogo validator annotation, it returns a nil.
In case some of the fields are annotated, the Validate function returns nil upon sucessful validation, or an error
describing why the validation failed.
The Validate method is called recursively for all submessage of the message.

TODO(michal): ADD COMMENTS.

Equal is enabled using the following extensions:

  - equal
  - equal_all

While VerboseEqual is enable dusing the following extensions:

  - verbose_equal
  - verbose_equal_all

The equal plugin also generates a test given it is enabled using one of the following extensions:

  - testgen
  - testgen_all

Let us look at:

  github.com/gogo/protobuf/test/example/example.proto

Btw all the output can be seen at:

  github.com/gogo/protobuf/test/example/*

The following message:



given to the equal plugin, will generate the following code:



and the following test code:


*/
package plugin

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/gogo/protobuf/gogoproto"
	gogo "github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/generator"
	"github.com/gogo/protobuf/vanity"
	"github.com/golang/protobuf/proto"
	descriptor "github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/mwitkow/go-proto-validators"
)

type plugin struct {
	*generator.Generator
	generator.PluginImports
	regexPkg      generator.Single
	fmtPkg        generator.Single
	protoPkg      generator.Single
	validatorPkg  generator.Single
	useGogoImport bool
	ruleset       string
	regexVars     map[string]bool
}

func NewPlugin(useGogoImport bool) generator.Plugin {
	return &plugin{useGogoImport: useGogoImport, regexVars: map[string]bool{}}
}

func (p *plugin) Name() string {
	return "validator"
}

func (p *plugin) Init(g *generator.Generator) {
	p.Generator = g
}

func (p *plugin) Generate(file *generator.FileDescriptor) {
	if !p.useGogoImport {
		vanity.TurnOffGogoImport(file.FileDescriptorProto)
	}
	p.PluginImports = generator.NewPluginImports(p.Generator)
	p.regexPkg = p.NewImport("regexp")
	p.fmtPkg = p.NewImport("fmt")
	p.validatorPkg = p.NewImport("github.com/mwitkow/go-proto-validators")

	for _, parseRuleSets := range []bool{true, false} {
		for _, msg := range file.Messages() {
			p.ruleset = ""
			if msg.DescriptorProto.GetOptions().GetMapEntry() {
				continue
			}
			if !parseRuleSets {
				p.generateRegexVars(file, msg)
				if gogoproto.IsProto3(file.FileDescriptorProto) {
					p.generateProto3Message(file, msg)
				} else {
					p.generateProto2Message(file, msg)
				}
				continue
			}
			// Checking for messages that are self-declared rule sets.
			pd := asDesc(msg.DescriptorProto)
			v, err := proto.GetExtension(pd.Options, validator.E_Rules)
			if err != nil || v.(*bool) == nil {
				continue
			}
			if b := v.(*bool); !*b {
				continue
			}
			// We have a rule set message, parse it.
			// Keep track of the rules in this set so that we can reference them later.
			p.ruleset = generator.CamelCaseSlice(msg.TypeName())
			p.generateRegexVars(file, msg)
			p.P(`var `, `_rules_`, p.ruleset, ` = map[string]func(interface{}) error {`)
			p.In()
			if gogoproto.IsProto3(file.FileDescriptorProto) {
				p.generateProto3Message(file, msg)
			} else {
				p.generateProto2Message(file, msg)
			}
			p.Out()
			p.P(`}`)
		}
	}
}

func getFieldValidatorIfAny(field *descriptor.FieldDescriptorProto) *validator.FieldValidator {
	if field.Options != nil {
		v, err := proto.GetExtension(field.Options, validator.E_Field)
		if err == nil && v.(*validator.FieldValidator) != nil {
			return (v.(*validator.FieldValidator))
		}
	}
	return nil
}

func (p *plugin) isSupportedInt(field *descriptor.FieldDescriptorProto) bool {
	switch *(field.Type) {
	case descriptor.FieldDescriptorProto_TYPE_INT32, descriptor.FieldDescriptorProto_TYPE_INT64:
		return true
	case descriptor.FieldDescriptorProto_TYPE_UINT32, descriptor.FieldDescriptorProto_TYPE_UINT64:
		return true
	case descriptor.FieldDescriptorProto_TYPE_SINT32, descriptor.FieldDescriptorProto_TYPE_SINT64:
		return true
	}
	return false
}

func (p *plugin) isSupportedFloat(field *descriptor.FieldDescriptorProto) bool {
	switch *(field.Type) {
	case descriptor.FieldDescriptorProto_TYPE_FLOAT, descriptor.FieldDescriptorProto_TYPE_DOUBLE:
		return true
	case descriptor.FieldDescriptorProto_TYPE_FIXED32, descriptor.FieldDescriptorProto_TYPE_FIXED64:
		return true
	case descriptor.FieldDescriptorProto_TYPE_SFIXED32, descriptor.FieldDescriptorProto_TYPE_SFIXED64:
		return true
	}
	return false
}

func asField(m gogo.Message) *descriptor.FieldDescriptorProto {
	data, err := gogo.Marshal(m)
	if err != nil {
		panic(err)
	}
	var f descriptor.FieldDescriptorProto
	err = proto.Unmarshal(data, &f)
	if err != nil {
		panic(err)
	}
	return &f
}

func asDesc(m gogo.Message) *descriptor.DescriptorProto {
	data, err := gogo.Marshal(m)
	if err != nil {
		panic(err)
	}
	var f descriptor.DescriptorProto
	err = proto.Unmarshal(data, &f)
	if err != nil {
		panic(err)
	}
	return &f
}

func (p *plugin) generateRegexVars(file *generator.FileDescriptor, message *generator.Descriptor) {
	ccTypeName := generator.CamelCaseSlice(message.TypeName())
	for _, field := range message.Field {
		validator := getFieldValidatorIfAny(asField(field))
		if validator != nil && validator.Regex != nil {
			fieldName := p.GetOneOfFieldName(message, field)
			vname := p.regexName(ccTypeName, fieldName)
			if p.regexVars[vname] {
				continue
			}
			p.P(`var `, vname, ` = `, p.regexPkg.Use(), `.MustCompile(`, "`", *validator.Regex, "`", `)`)
			p.regexVars[vname] = true
		}
	}
}

func (p *plugin) generateProto2Message(file *generator.FileDescriptor, message *generator.Descriptor) {
	ccTypeName := generator.CamelCaseSlice(message.TypeName())

	if p.ruleset == "" {
		p.P(`func (this *`, ccTypeName, `) Validate() error {`)
		p.In()
	}
	for _, field := range message.Field {
		fieldName := p.GetFieldName(message, field)
		f := asField(field)
		fieldValidator := getFieldValidatorIfAny(f)
		if fieldValidator == nil && !field.IsMessage() {
			continue
		}
		if p.validatorWithMessageExists(fieldValidator) {
			fmt.Fprintf(os.Stderr, "WARNING: field %v.%v is a proto2 message, validator.msg_exists has no effect\n", ccTypeName, fieldName)
		}
		if fieldValidator.GetRules() != "" && p.ruleset != "" {
			// We don't support this yet because there are no checks for
			// infinite graph loops.
			fmt.Fprintf(os.Stderr, "WARNING: rule-set messages may not specify `rules` fields: %v.%v\n", ccTypeName, fieldName)
			continue

		}
		variableName := "this." + fieldName
		p.generateRulesetValidator(variableName, ccTypeName, fieldName, fieldValidator)
		if p.ruleset != "" {
			variableName = fieldName
			t, _ := p.GoType(message, field)
			p.P(`"`, field.Name, `": func(_`, fieldName, ` interface{}) error {`)
			p.In()
			p.P(fieldName, `, _ := _`, fieldName, `.(`, t, `)`)
		}
		repeated := field.IsRepeated()
		nullable := gogoproto.IsNullable(field)
		// For proto2 syntax, only Gogo generates non-pointer fields
		nonpointer := gogoproto.ImportsGoGoProto(file.FileDescriptorProto) && !gogoproto.IsNullable(field)
		if repeated {
			p.generateRepeatedCountValidator(variableName, ccTypeName, fieldName, fieldValidator)
			if field.IsMessage() || p.validatorWithNonRepeatedConstraint(fieldValidator) {
				p.P(`for _, item := range `, variableName, `{`)
				p.In()
				variableName = "item"
			}
		} else if nullable {
			p.P(`if `, variableName, ` != nil {`)
			p.In()
			if !field.IsBytes() {
				variableName = "*(" + variableName + ")"
			}
		} else if nonpointer {
			// can use the field directly
		} else if !field.IsMessage() && p.ruleset == "" {
			variableName = `this.Get` + fieldName + `()`
		}
		if !repeated && fieldValidator != nil {
			if fieldValidator.RepeatedCountMin != nil {
				fmt.Fprintf(os.Stderr, "WARNING: field %v.%v is not repeated, validator.min_elts has no effects\n", ccTypeName, fieldName)
			}
			if fieldValidator.RepeatedCountMax != nil {
				fmt.Fprintf(os.Stderr, "WARNING: field %v.%v is not repeated, validator.max_elts has no effects\n", ccTypeName, fieldName)
			}
		}
		if field.IsString() {
			p.generateStringValidator(variableName, ccTypeName, fieldName, fieldValidator)
		} else if p.isSupportedInt(f) {
			p.generateIntValidator(variableName, ccTypeName, fieldName, fieldValidator)
		} else if p.isSupportedFloat(f) {
			p.generateFloatValidator(variableName, ccTypeName, fieldName, fieldValidator)
		} else if field.IsBytes() {
			p.generateLengthValidator(variableName, ccTypeName, fieldName, fieldValidator)
		} else if field.IsMessage() {
			if repeated && nullable {
				variableName = "*(item)"
			}
			p.P(`if err := `, p.validatorPkg.Use(), `.CallValidatorIfExists(&(`, variableName, `)); err != nil {`)
			p.In()
			p.P(`return `, p.validatorPkg.Use(), `.FieldError("`, fieldName, `", err)`)
			p.Out()
			p.P(`}`)
		}
		if repeated {
			// end the repeated loop
			if field.IsMessage() || p.validatorWithNonRepeatedConstraint(fieldValidator) {
				// This internal 'if' cannot be refactored as it would change semantics with respect to the corresponding prelude 'if's
				p.Out()
				p.P(`}`)
			}
		} else if nullable {
			// end the if around nullable
			p.Out()
			p.P(`}`)
		}
		if p.ruleset != "" {
			p.P(`return nil`)
			p.Out()
			p.P(`},`)
		}
	}
	if p.ruleset == "" {
		p.P(`return nil`)
		p.Out()
		p.P(`}`)
	}
}

func (p *plugin) generateProto3Message(file *generator.FileDescriptor, message *generator.Descriptor) {
	ccTypeName := generator.CamelCaseSlice(message.TypeName())
	if p.ruleset == "" {
		p.P(`func (this *`, ccTypeName, `) Validate() error {`)
		p.In()
	}
	for _, field := range message.Field {
		f := asField(field)
		fieldValidator := getFieldValidatorIfAny(f)
		if fieldValidator == nil && !field.IsMessage() {
			continue
		}
		fieldName := p.GetOneOfFieldName(message, field)
		if fieldValidator.GetRules() != "" && p.ruleset != "" {
			// We don't support this yet because there are no checks for
			// infinite graph loops.
			fmt.Fprintf(os.Stderr, "WARNING: rule-set messages may not specify `rules` fields: %v.%v\n", ccTypeName, fieldName)
			continue

		}
		isOneOf := field.OneofIndex != nil
		variableName := "this." + fieldName

		p.generateRulesetValidator(variableName, ccTypeName, fieldName, fieldValidator)

		repeated := field.IsRepeated()
		// Golang's proto3 has no concept of unset primitive fields
		nullable := (gogoproto.IsNullable(field) || !gogoproto.ImportsGoGoProto(file.FileDescriptorProto)) && field.IsMessage()
		if p.fieldIsProto3Map(file, message, f) {
			p.P(`// Validation of proto3 map<> fields is unsupported.`)
			continue
		}
		if p.ruleset != "" {
			variableName = fieldName
			t, _ := p.GoType(message, field)
			p.P(`"`, field.Name, `": func(_`, fieldName, ` interface{}) error {`)
			p.In()
			p.P(fieldName, `, _ := _`, fieldName, `.(`, t, `)`)
		}
		if isOneOf {
			// XXX does not support rule validation
			p.In()
			oneOfName := p.GetFieldName(message, field)
			oneOfType := p.OneOfTypeName(message, field)
			//if x, ok := m.GetType().(*OneOfMessage3_OneInt); ok {
			p.P(`if oneOfNester, ok := this.Get` + oneOfName + `().(* ` + oneOfType + `); ok {`)
			variableName = "oneOfNester." + p.GetOneOfFieldName(message, field)
		}
		if repeated {
			p.generateRepeatedCountValidator(variableName, ccTypeName, fieldName, fieldValidator)
			if field.IsMessage() || p.validatorWithNonRepeatedConstraint(fieldValidator) {
				p.P(`for _, item := range `, variableName, `{`)
				p.In()
				variableName = "item"
			}
		} else if fieldValidator != nil {
			if fieldValidator.RepeatedCountMin != nil {
				fmt.Fprintf(os.Stderr, "WARNING: field %v.%v is not repeated, validator.min_elts has no effects\n", ccTypeName, fieldName)
			}
			if fieldValidator.RepeatedCountMax != nil {
				fmt.Fprintf(os.Stderr, "WARNING: field %v.%v is not repeated, validator.max_elts has no effects\n", ccTypeName, fieldName)
			}
		}
		if field.IsString() {
			p.generateStringValidator(variableName, ccTypeName, fieldName, fieldValidator)
		} else if p.isSupportedInt(f) {
			p.generateIntValidator(variableName, ccTypeName, fieldName, fieldValidator)
		} else if p.isSupportedFloat(f) {
			p.generateFloatValidator(variableName, ccTypeName, fieldName, fieldValidator)
		} else if field.IsBytes() {
			p.generateLengthValidator(variableName, ccTypeName, fieldName, fieldValidator)
		} else if field.IsMessage() {
			if p.validatorWithMessageExists(fieldValidator) {
				if nullable && !repeated {
					p.P(`if nil == `, variableName, `{`)
					p.In()
					p.P(`return `, p.validatorPkg.Use(), `.FieldError("`, fieldName, `",`, p.fmtPkg.Use(), `.Errorf("message must exist"))`)
					p.Out()
					p.P(`}`)
				} else if repeated {
					fmt.Fprintf(os.Stderr, "WARNING: field %v.%v is repeated, validator.msg_exists has no effect\n", ccTypeName, fieldName)
				} else if !nullable {
					fmt.Fprintf(os.Stderr, "WARNING: field %v.%v is a nullable=false, validator.msg_exists has no effect\n", ccTypeName, fieldName)
				}
			}
			nullcheck := nullable && p.ruleset == ""
			if nullcheck {
				p.P(`if `, variableName, ` != nil {`)
				p.In()
			} else if p.ruleset == "" {
				// non-nullable fields in proto3 store actual structs, we need pointers to operate on interfaces
				variableName = "&(" + variableName + ")"
			}
			p.P(`if err := `, p.validatorPkg.Use(), `.CallValidatorIfExists(`, variableName, `); err != nil {`)
			p.In()
			p.P(`return `, p.validatorPkg.Use(), `.FieldError("`, fieldName, `", err)`)
			p.Out()
			p.P(`}`)
			if nullcheck {
				p.Out()
				p.P(`}`)
			}
		}
		if repeated && (field.IsMessage() || p.validatorWithNonRepeatedConstraint(fieldValidator)) {
			// end the repeated loop
			p.Out()
			p.P(`}`)
		}
		if isOneOf {
			// end the oneof if statement
			p.Out()
			p.P(`}`)
		}
		if p.ruleset != "" {
			p.P(`return nil`)
			p.Out()
			p.P(`},`)
		}
	}
	if p.ruleset == "" {
		p.P(`return nil`)
		p.Out()
		p.P(`}`)
	}
}

func (p *plugin) generateRulesetValidator(variableName, ccTypeName, fieldName string, fv *validator.FieldValidator) {
	rules := fv.GetRules()
	if rules == "" {
		return
	}
	// Look up rule set and apply them here
	//
	// rule set references look like:
	//   Message.field
	// or
	//   Message.SubMessage.field
	// essentially
	//   <dot_separated_message_type>.<field>
	//
	// TODO(jdef) add type-checking at protoc-compile time so that
	// rules are only applied to fields of the same type as that for
	// which the rule was originally declared.
	ns := strings.SplitN(rules, ".", -1)
	if len(ns) < 2 {
		fmt.Fprintf(os.Stderr, "WARNING: field %v.%v references a ruleset using invalid syntax %q\n", ccTypeName, fieldName, rules)
		return
	}
	rsName := `_rules_` + generator.CamelCaseSlice(ns[:len(ns)-1])
	ruleField := ns[len(ns)-1]
	p.P(`if ff, ok := `, rsName, `["`, ruleField, `"]; ok {`)
	p.In()
	p.P(`if err := ff(`, variableName, `); err != nil {`)
	p.In()
	p.P(`fe, ok := err.(`, p.validatorPkg.Use(), `.Replacer)`)
	p.P(`if !ok {`)
	p.In()
	p.P(`return err`)
	p.Out()
	p.P(`}`)
	p.P(`return fe.Replace(0,"`, fieldName, `")`)
	p.Out()
	p.P(`}`)
	p.Out()
	p.P(`}`)
}

func (p *plugin) generateIntValidator(variableName string, ccTypeName string, fieldName string, fv *validator.FieldValidator) {
	if fv.IntGt != nil {
		p.P(`if !(`, variableName, ` > `, fv.IntGt, `) {`)
		p.In()
		errorStr := fmt.Sprintf(`be greater than '%d'`, fv.GetIntGt())
		p.generateErrorString(variableName, fieldName, errorStr, fv)
		p.Out()
		p.P(`}`)
	}
	if fv.IntLt != nil {
		p.P(`if !(`, variableName, ` < `, fv.IntLt, `) {`)
		p.In()
		errorStr := fmt.Sprintf(`be less than '%d'`, fv.GetIntLt())
		p.generateErrorString(variableName, fieldName, errorStr, fv)
		p.Out()
		p.P(`}`)
	}
}

func (p *plugin) generateLengthValidator(variableName string, ccTypeName string, fieldName string, fv *validator.FieldValidator) {
	if fv.LengthGt != nil {
		p.P(`if !( len(`, variableName, `) > `, fv.LengthGt, `) {`)
		p.In()
		errorStr := fmt.Sprintf(`length be greater than '%d'`, fv.GetLengthGt())
		p.generateErrorString(variableName, fieldName, errorStr, fv)
		p.Out()
		p.P(`}`)
	}

	if fv.LengthLt != nil {
		p.P(`if !( len(`, variableName, `) < `, fv.LengthLt, `) {`)
		p.In()
		errorStr := fmt.Sprintf(`length be less than '%d'`, fv.GetLengthLt())
		p.generateErrorString(variableName, fieldName, errorStr, fv)
		p.Out()
		p.P(`}`)
	}

	if fv.LengthEq != nil {
		p.P(`if !( len(`, variableName, `) == `, fv.LengthEq, `) {`)
		p.In()
		errorStr := fmt.Sprintf(`length be not equal '%d'`, fv.GetLengthEq())
		p.generateErrorString(variableName, fieldName, errorStr, fv)
		p.Out()
		p.P(`}`)
	}

}

func (p *plugin) generateFloatValidator(variableName string, ccTypeName string, fieldName string, fv *validator.FieldValidator) {
	upperIsStrict := true
	lowerIsStrict := true

	// First check for incompatible constraints (i.e flt_lt & flt_lte both defined, etc) and determine the real limits.
	if fv.FloatEpsilon != nil && fv.FloatLt == nil && fv.FloatGt == nil {
		fmt.Fprintf(os.Stderr, "WARNING: field %v.%v has no 'float_lt' or 'float_gt' field so setting 'float_epsilon' has no effect.", ccTypeName, fieldName)
	}
	if fv.FloatLt != nil && fv.FloatLte != nil {
		fmt.Fprintf(os.Stderr, "WARNING: field %v.%v has both 'float_lt' and 'float_lte' constraints, only the strictest will be used.", ccTypeName, fieldName)
		strictLimit := fv.GetFloatLt()
		if fv.FloatEpsilon != nil {
			strictLimit += fv.GetFloatEpsilon()
		}

		if fv.GetFloatLte() < strictLimit {
			upperIsStrict = false
		}
	} else if fv.FloatLte != nil {
		upperIsStrict = false
	}

	if fv.FloatGt != nil && fv.FloatGte != nil {
		fmt.Fprintf(os.Stderr, "WARNING: field %v.%v has both 'float_gt' and 'float_gte' constraints, only the strictest will be used.", ccTypeName, fieldName)
		strictLimit := fv.GetFloatGt()
		if fv.FloatEpsilon != nil {
			strictLimit -= fv.GetFloatEpsilon()
		}

		if fv.GetFloatGte() > strictLimit {
			lowerIsStrict = false
		}
	} else if fv.FloatGte != nil {
		lowerIsStrict = false
	}

	// Generate the constraint checking code.
	errorStr := ""
	compareStr := ""
	if fv.FloatGt != nil || fv.FloatGte != nil {
		compareStr = fmt.Sprint(`if !(`, variableName)
		if lowerIsStrict {
			errorStr = fmt.Sprintf(`be strictly greater than '%g'`, fv.GetFloatGt())
			if fv.FloatEpsilon != nil {
				errorStr += fmt.Sprintf(` with a tolerance of '%g'`, fv.GetFloatEpsilon())
				compareStr += fmt.Sprint(` + `, fv.GetFloatEpsilon())
			}
			compareStr += fmt.Sprint(` > `, fv.GetFloatGt(), `) {`)
		} else {
			errorStr = fmt.Sprintf(`be greater than or equal to '%g'`, fv.GetFloatGte())
			compareStr += fmt.Sprint(` >= `, fv.GetFloatGte(), `) {`)
		}
		p.P(compareStr)
		p.In()
		p.generateErrorString(variableName, fieldName, errorStr, fv)
		p.Out()
		p.P(`}`)
	}

	if fv.FloatLt != nil || fv.FloatLte != nil {
		compareStr = fmt.Sprint(`if !(`, variableName)
		if upperIsStrict {
			errorStr = fmt.Sprintf(`be strictly lower than '%g'`, fv.GetFloatLt())
			if fv.FloatEpsilon != nil {
				errorStr += fmt.Sprintf(` with a tolerance of '%g'`, fv.GetFloatEpsilon())
				compareStr += fmt.Sprint(` - `, fv.GetFloatEpsilon())
			}
			compareStr += fmt.Sprint(` < `, fv.GetFloatLt(), `) {`)
		} else {
			errorStr = fmt.Sprintf(`be lower than or equal to '%g'`, fv.GetFloatLte())
			compareStr += fmt.Sprint(` <= `, fv.GetFloatLte(), `) {`)
		}
		p.P(compareStr)
		p.In()
		p.generateErrorString(variableName, fieldName, errorStr, fv)
		p.Out()
		p.P(`}`)
	}
}

func (p *plugin) generateStringValidator(variableName string, ccTypeName string, fieldName string, fv *validator.FieldValidator) {
	if fv.Regex != nil {
		p.P(`if !`, p.regexName(ccTypeName, fieldName), `.MatchString(`, variableName, `) {`)
		p.In()
		errorStr := "be a string conforming to regex " + strconv.Quote(fv.GetRegex())
		p.generateErrorString(variableName, fieldName, errorStr, fv)
		p.Out()
		p.P(`}`)
	}
	if fv.StringNotEmpty != nil && fv.GetStringNotEmpty() {
		p.P(`if `, variableName, ` == "" {`)
		p.In()
		errorStr := "not be an empty string"
		p.generateErrorString(variableName, fieldName, errorStr, fv)
		p.Out()
		p.P(`}`)
	}
	p.generateLengthValidator(variableName, ccTypeName, fieldName, fv)

}

func (p *plugin) generateRepeatedCountValidator(variableName string, ccTypeName string, fieldName string, fv *validator.FieldValidator) {
	if fv == nil {
		return
	}
	if fv.RepeatedCountMin != nil {
		compareStr := fmt.Sprint(`if len(`, variableName, `) < `, fv.GetRepeatedCountMin(), ` {`)
		p.P(compareStr)
		p.In()
		errorStr := fmt.Sprint(`contain at least `, fv.GetRepeatedCountMin(), ` elements`)
		p.generateErrorString(variableName, fieldName, errorStr, fv)
		p.Out()
		p.P(`}`)
	}
	if fv.RepeatedCountMax != nil {
		compareStr := fmt.Sprint(`if len(`, variableName, `) > `, fv.GetRepeatedCountMax(), ` {`)
		p.P(compareStr)
		p.In()
		errorStr := fmt.Sprint(`contain at most `, fv.GetRepeatedCountMax(), ` elements`)
		p.generateErrorString(variableName, fieldName, errorStr, fv)
		p.Out()
		p.P(`}`)
	}
}

func (p *plugin) generateErrorString(variableName string, fieldName string, specificError string, fv *validator.FieldValidator) {
	if fv.GetHumanError() == "" {
		p.P(`return `, p.validatorPkg.Use(), `.FieldError("`, fieldName, `",`, p.fmtPkg.Use(), ".Errorf(`value '%v' must ", specificError, "`", `, `, variableName, `))`)
	} else {
		p.P(`return `, p.validatorPkg.Use(), `.FieldError("`, fieldName, `",`, p.fmtPkg.Use(), ".Errorf(`", fv.GetHumanError(), "`))")
	}

}

func (p *plugin) fieldIsProto3Map(file *generator.FileDescriptor, message *generator.Descriptor, field *descriptor.FieldDescriptorProto) bool {
	// Context from descriptor.proto
	// Whether the message is an automatically generated map entry type for the
	// maps field.
	//
	// For maps fields:
	//     map<KeyType, ValueType> map_field = 1;
	// The parsed descriptor looks like:
	//     message MapFieldEntry {
	//         option map_entry = true;
	//         optional KeyType key = 1;
	//         optional ValueType value = 2;
	//     }
	//     repeated MapFieldEntry map_field = 1;
	//
	// Implementations may choose not to generate the map_entry=true message, but
	// use a native map in the target language to hold the keys and values.
	// The reflection APIs in such implementions still need to work as
	// if the field is a repeated message field.
	//
	// NOTE: Do not set the option in .proto files. Always use the maps syntax
	// instead. The option should only be implicitly set by the proto compiler
	// parser.
	repeated := field.Label != nil && *field.Label == descriptor.FieldDescriptorProto_LABEL_REPEATED
	if field.GetType() != descriptor.FieldDescriptorProto_TYPE_MESSAGE || !repeated {
		return false
	}
	typeName := field.GetTypeName()
	var msg *descriptor.DescriptorProto
	if strings.HasPrefix(typeName, ".") {
		// Fully qualified case, look up in global map, must work or fail badly.
		msg = asDesc(p.ObjectNamed(field.GetTypeName()).(*generator.Descriptor).DescriptorProto)
	} else {
		// Nested, relative case.
		msg = asDesc(file.GetNestedMessage(message.DescriptorProto, field.GetTypeName()))
	}
	return msg.GetOptions().GetMapEntry()
}

func (p *plugin) validatorWithAnyConstraint(fv *validator.FieldValidator) bool {
	if fv == nil {
		return false
	}

	// Need to use reflection in order to be future-proof for new types of constraints.
	v := reflect.ValueOf(fv)
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Interface() != nil {
			return true
		}
	}
	return false
}

func (p *plugin) validatorWithMessageExists(fv *validator.FieldValidator) bool {
	return fv != nil && fv.MsgExists != nil && *(fv.MsgExists)
}

func (p *plugin) validatorWithNonRepeatedConstraint(fv *validator.FieldValidator) bool {
	if fv == nil {
		return false
	}

	// Need to use reflection in order to be future-proof for new types of constraints.
	v := reflect.ValueOf(*fv)
	for i := 0; i < v.NumField(); i++ {
		n := v.Type().Field(i).Name
		// TODO(jdef) there's probably a more elegant way to ignore the auto-gen'd
		// XXX_ extra fields, but this stopped the test cases from panicking.
		if strings.HasPrefix(n, "XXX_") {
			continue
		}
		val := func() (b bool) {
			success := false
			defer func() {
				if !success {
					println("failed to check field " + n)
				}
			}()
			b = n != "RepeatedCountMin" && n != "RepeatedCountMax" && v.Field(i).Pointer() != 0
			success = true
			return
		}()
		if val {
			return true
		}
	}
	return false
}

func (p *plugin) regexName(ccTypeName string, fieldName string) string {
	return "_regex_" + ccTypeName + "_" + fieldName
}
