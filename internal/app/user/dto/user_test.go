package dto

import (
	"reflect"
	"testing"
)

func TestRegisterRequestDoesNotExposeName(t *testing.T) {
	requestType := reflect.TypeOf(RegisterRequest{})
	if _, ok := requestType.FieldByName("Name"); ok {
		t.Fatal("RegisterRequest should not expose name during registration")
	}
}
