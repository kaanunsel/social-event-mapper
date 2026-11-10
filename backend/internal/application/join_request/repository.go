package join_request

import (
	"context"

	"github.com/google/uuid"
	"github.com/kaanunsel/social-event-mapper/backend/internal/domain"
)

// Repository is the application-layer persistence port for join requests.
type Repository interface {
	CreateJoinRequest(ctx context.Context, params CreateJoinRequestParams) (*domain.JoinRequest, error)
	ApproveJoinRequest(ctx context.Context, params ApproveJoinRequestParams) (*ApproveJoinRequestResult, error)
	RejectJoinRequest(ctx context.Context, params RejectJoinRequestParams) (*RejectJoinRequestResult, error)
	CancelJoinRequestByUser(ctx context.Context, params CancelJoinRequestByUserParams) (*domain.JoinRequest, error)
	GetNotificationContext(ctx context.Context, joinRequestID uuid.UUID) (*NotificationContext, error)
}
