package model

import (
	"reflect"
	"strings"
	"testing"
)

func TestOrderItemIDHasUniqueIndex(t *testing.T) {
	field, ok := reflect.TypeOf(Order{}).FieldByName("ItemID")
	if !ok {
		t.Fatal("Order.ItemID field missing")
	}
	gormTag := field.Tag.Get("gorm")
	if !strings.Contains(gormTag, "uniqueIndex") {
		t.Fatalf("ItemID gorm tag should enforce one order per item, got %q", gormTag)
	}
}
