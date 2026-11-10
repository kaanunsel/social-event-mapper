package participation

import (
	"context"

	"github.com/google/uuid"
	"github.com/kaanunsel/social-event-mapper/backend/internal/domain"
)

// Repository is the application-layer persistence port for participations.
type Repository interface {
	CreateParticipation(ctx context.Context, eventID, userID uuid.UUID) (*domain.Participation, error)
	LeaveParticipation(ctx context.Context, eventID, userID uuid.UUID) (*domain.Participation, error)
	CancelEventParticipations(ctx context.Context, eventID uuid.UUID) ([]uuid.UUID, error)
	ListApprovedParticipantUserIDs(ctx context.Context, eventID uuid.UUID) ([]uuid.UUID, error)
	MarkApprovedParticipationsPending(ctx context.Context, eventID, hostUserID uuid.UUID) ([]uuid.UUID, error)
	ReconfirmParticipation(ctx context.Context, eventID, userID uuid.UUID, eventVersion int) (*domain.Participation, error)
	ApprovePendingParticipationsForEvent(ctx context.Context, eventID uuid.UUID) error
}
