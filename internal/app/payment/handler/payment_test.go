package handler

import (
	"context"
	"testing"

	usermodel "github.com/zet-plane/live-auction-backend/internal/app/user/model"
)

type fakeOrderOperator struct{}

func (f *fakeOrderOperator) Pay(_ context.Context, _ *usermodel.User, _ string) error {
	return nil
}

func (f *fakeOrderOperator) Cancel(_ context.Context, _ *usermodel.User, _ string) error {
	return nil
}

func TestInitAcceptsOrderOperatorInterface(t *testing.T) {
	Init(&fakeOrderOperator{})
}
