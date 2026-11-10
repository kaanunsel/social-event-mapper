package category

import (
	"context"

	"github.com/kaanunsel/social-event-mapper/backend/internal/domain"
)

// Repository is the application-layer persistence port for category flows.
type Repository interface {
	ListCategories(ctx context.Context) ([]domain.EventCategory, error)
}
