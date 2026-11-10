package participation

import (
	"context"

	"github.com/google/uuid"
	"github.com/kaanunsel/social-event-mapper/backend/internal/domain"
)

// UseCase is the inbound application port for participation flows.
type UseCase interface {
	CreateApprovedParticipation(ctx context.Context, eventID, userID uuid.UUID) (*domain.Participation, error)
	LeaveParticipation(ctx context.Context, eventID, userID uuid.UUID) (*domain.Participation, error)
	CancelEventParticipations(ctx context.Context, eventID uuid.UUID) ([]uuid.UUID, error)
	EvaluateBadgesForEventParticipants(ctx context.Context, eventID uuid.UUID) error
	MarkApprovedParticipationsPending(ctx context.Context, eventID, hostUserID uuid.UUID) ([]uuid.UUID, error)
	ReconfirmParticipation(ctx context.Context, eventID, userID uuid.UUID, eventVersion int) (*domain.Participation, error)
	ApprovePendingParticipationsForEvent(ctx context.Context, eventID uuid.UUID) error
}
