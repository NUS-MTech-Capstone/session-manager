package service

import (
	"context"
	"strconv"

	"github.com/NUS-MTech-Capstone/session-manager/internal/model"
)

func (s *Service) AppendSessionAuditRecords(
	ctx context.Context,
	sessionID string,
	req model.AppendSessionAuditRecordsRequest,
) ([]model.SessionAuditRecord, error) {
	return s.store.AppendSessionAuditRecords(ctx, sessionID, req, s.auditRetention, s.auditMaxBytes)
}

func (s *Service) ListSessionAuditRecords(
	ctx context.Context,
	sessionID string,
	filter model.ListSessionAuditRecordsFilter,
) (model.ListSessionAuditRecordsResponse, error) {
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 100
	}
	records, err := s.store.ListSessionAuditRecords(ctx, sessionID, filter, s.auditRetention)
	if err != nil {
		return model.ListSessionAuditRecordsResponse{}, err
	}
	response := model.ListSessionAuditRecordsResponse{Records: records}
	if len(records) == filter.Limit {
		response.NextCursor = strconv.FormatInt(records[len(records)-1].Sequence, 10)
	}
	return response, nil
}

func (s *Service) ListAuditSessions(
	ctx context.Context,
	filter model.ListAuditSessionsFilter,
) (model.ListAuditSessionsResponse, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	sessions, err := s.store.ListAuditSessions(ctx, filter, s.auditRetention)
	if err != nil {
		return model.ListAuditSessionsResponse{}, err
	}
	response := model.ListAuditSessionsResponse{Sessions: sessions}
	if len(sessions) == filter.Limit {
		response.NextCursor = strconv.Itoa(filter.Offset + len(sessions))
	}
	return response, nil
}
