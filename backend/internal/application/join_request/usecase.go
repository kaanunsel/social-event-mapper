package join_request

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/kaanunsel/social-event-mapper/backend/internal/domain"
)

// UseCase is the inbound application port for join-request flows.
type UseCase interface {
	CreatePendingJoinRequest(ctx context.Context, eventID, userID, hostUserID uuid.UUID, input CreatePendingJoinRequestInput) (*domain.JoinRequest, error)
	ApproveJoinRequest(ctx context.Context, eventID, joinRequestID, hostUserID uuid.UUID) (*ApproveJoinRequestResult, error)
	RejectJoinRequest(ctx context.Context, eventID, joinRequestID, hostUserID uuid.UUID) (*RejectJoinRequestResult, error)
	CancelJoinRequest(ctx context.Context, eventID, userID uuid.UUID) (*domain.JoinRequest, error)
}

// ApproveJoinRequestResult contains the persisted moderation state after approval.
type ApproveJoinRequestResult struct {
	JoinRequest   *domain.JoinRequest
	Participation *domain.Participation
}

// RejectJoinRequestResult contains the persisted moderation state after rejection.
type RejectJoinRequestResult struct {
	JoinRequest    *domain.JoinRequest
	CooldownEndsAt time.Time
}
