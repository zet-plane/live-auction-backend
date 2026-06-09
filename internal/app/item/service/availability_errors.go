package service

import (
	"net/http"

	"github.com/zet-plane/live-auction-backend/pkg/errorx"
)

var ErrAvailabilityUnavailable = errorx.New(http.StatusServiceUnavailable, 50301, "auction temporarily unavailable")
