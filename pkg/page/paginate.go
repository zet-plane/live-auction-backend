package page

import "gorm.io/gorm"

// PageRequest holds pagination parameters parsed from query strings.
type PageRequest struct {
	Page     int
	PageSize int
}

// Scope returns a GORM scope applying the pagination offsets.
func (p PageRequest) Scope() func(*gorm.DB) *gorm.DB {
	return Paginate(p.Page, p.PageSize)
}

// Paginate is a low-level GORM scope used directly when no PageRequest is available.
func Paginate(page, limit int) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if page <= 0 {
			page = 1
		}
		switch {
		case limit > 100:
			limit = 100
		case limit <= 0:
			limit = 10
		}
		return db.Offset((page - 1) * limit).Limit(limit)
	}
}
