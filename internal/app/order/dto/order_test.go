package dto

import (
	"reflect"
	"strings"
	"testing"
)

func TestPayOrderRequestResultRequiresSuccessEnum(t *testing.T) {
	field, ok := reflect.TypeOf(PayOrderRequest{}).FieldByName("Result")
	if !ok {
		t.Fatal("PayOrderRequest.Result field missing")
	}
	bindingTag := field.Tag.Get("binding")
	if !strings.Contains(bindingTag, "required") {
		t.Fatalf("Result binding tag should require a value, got %q", bindingTag)
	}
	if !strings.Contains(bindingTag, "oneof=success") {
		t.Fatalf("Result binding tag should only allow success, got %q", bindingTag)
	}
}
