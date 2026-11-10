package ticket

import (
	"context"

	"github.com/google/uuid"
	"github.com/kaanunsel/social-event-mapper/backend/internal/domain"
)

// Repository is the application-layer persistence port for ticket flows.
type Repository interface {
	CreateTicketForParticipation(ctx context.Context, participationID uuid.UUID, status domain.TicketStatus) (*domain.Ticket, error)
	CancelTicketForParticipation(ctx context.Context, participationID uuid.UUID) error
	CancelTicketsForEvent(ctx context.Context, eventID uuid.UUID) error
	ExpireTicketsForEvent(ctx context.Context, eventID uuid.UUID) error
	MarkTicketsPendingForEvent(ctx context.Context, eventID uuid.UUID) error
	ActivatePendingTicketsForEvent(ctx context.Context, eventID uuid.UUID) error
	ListTicketsByUser(ctx context.Context, userID uuid.UUID) ([]TicketRecord, error)
	GetTicketDetail(ctx context.Context, userID, ticketID uuid.UUID) (*TicketRecord, error)
	GetTicketAccessForUser(ctx context.Context, userID, ticketID uuid.UUID, lat, lon float64, forUpdate bool) (*TicketAccessRecord, error)
	StoreIssuedToken(ctx context.Context, ticketID uuid.UUID, version int, tokenHash string) error
	GetTicketForScan(ctx context.Context, eventID, ticketID uuid.UUID, forUpdate bool) (*TicketScanRecord, error)
	MarkTicketUsed(ctx context.Context, ticketID uuid.UUID) (*domain.Ticket, error)
}
