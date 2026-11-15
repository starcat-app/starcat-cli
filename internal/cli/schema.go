// 本文件验证 Starcat 公共能力契约使用的 JSON Schema 子集，服务端仍必须再次校验业务参数。
package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"unicode/utf8"
)

// validateSchema 不填默认值，避免悄悄把读取升级为联网或生成；未知约束交由 App 校验。
// CLI 只做结构、枚举、范围和数量预检，不复制 repo selector、权益等领域规则。
func validateSchema(schema map[string]any, value any, location string) error {
	if options, ok := schema["enum"].([]any); ok {
		matched := false
		for _, option := range options {
			if reflect.DeepEqual(option, value) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s is outside the declared enum", location)
		}
	}
	switch schema["type"] {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", location)
		}
		required, _ := schema["required"].([]any)
		for _, field := range required {
			name, _ := field.(string)
			if _, present := object[name]; !present {
				return fmt.Errorf("%s.%s is required", location, name)
			}
		}
		properties, _ := schema["properties"].(map[string]any)
		for name, child := range object {
			if childSchema, ok := properties[name].(map[string]any); ok {
				if err := validateSchema(childSchema, child, location+"."+name); err != nil {
					return err
				}
			} else if allowed, ok := schema["additionalProperties"].(bool); ok && !allowed {
				return fmt.Errorf("%s.%s is not declared", location, name)
			}
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", location)
		}
		if err := validateBounds(schema, float64(len(items)), "minItems", "maxItems", location); err != nil {
			return err
		}
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for index, item := range items {
				if err := validateSchema(itemSchema, item, fmt.Sprintf("%s[%d]", location, index)); err != nil {
					return err
				}
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", location)
		}
		return validateBounds(schema, float64(utf8.RuneCountInString(text)), "minLength", "maxLength", location)
	case "integer", "number":
		number, ok := schemaNumber(value)
		if !ok || (schema["type"] == "integer" && math.Trunc(number) != number) {
			return fmt.Errorf("%s must be %s", location, schema["type"])
		}
		return validateBounds(schema, number, "minimum", "maximum", location)
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", location)
		}
	}
	return nil
}

func validateBounds(schema map[string]any, value float64, minimum, maximum, location string) error {
	if bound, ok := schemaNumber(schema[minimum]); ok && value < bound {
		return fmt.Errorf("%s is below %s %g", location, minimum, bound)
	}
	if bound, ok := schemaNumber(schema[maximum]); ok && value > bound {
		return fmt.Errorf("%s exceeds %s %g", location, maximum, bound)
	}
	return nil
}

func schemaNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, !math.IsInf(number, 0) && !math.IsNaN(number)
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil && !math.IsInf(parsed, 0) && !math.IsNaN(parsed)
	default:
		return 0, false
	}
}
