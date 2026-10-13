package problems

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ACM-VIT/valence/gen/go/db"
	v1 "github.com/ACM-VIT/valence/gen/go/valence/v1"
	"github.com/ACM-VIT/valence/gen/go/valence/v1/valencev1connect"
	"github.com/ACM-VIT/valence/pkg/blob"
	"github.com/ACM-VIT/valence/services/api/internal/auth"
)

const (
	defaultPageSize = 50
	maxPageSize     = 100
)

type Service struct {
	pool  *pgxpool.Pool
	q     *db.Queries
	blobs blob.Store
}

var _ valencev1connect.ProblemServiceHandler = (*Service)(nil)

func NewService(pool *pgxpool.Pool, blobs blob.Store) *Service {
	return &Service{pool: pool, q: db.New(pool), blobs: blobs}
}

func canSeePrivate(ctx context.Context) bool {
	_, err := auth.RequireRole(ctx, "setter")
	return err == nil
}

func (s *Service) ListProblems(ctx context.Context, req *connect.Request[v1.ListProblemsRequest]) (*connect.Response[v1.ListProblemsResponse], error) {

	size := req.Msg.PageSize
	switch {
	case size < 0:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("page_size must not be negative"))
	case size == 0:
		size = defaultPageSize
	case size > maxPageSize:
		size = maxPageSize
	}

	var after int64
	if token := req.Msg.PageToken; token != "" {
		var err error
		after, err = strconv.ParseInt(token, 10, 64)
		if err != nil || after < 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid page_token"))
		}
	}

	rows, err := s.q.ListProblems(ctx, db.ListProblemsParams{
		IncludePrivate: canSeePrivate(ctx),
		AfterID:        after,
		PageSize:       size + 1,
	})
	if err != nil {
		return nil, internal(ctx, err)
	}

	res := &v1.ListProblemsResponse{}
	if len(rows) > int(size) {
		rows = rows[:size]
		res.NextPageToken = strconv.FormatInt(rows[len(rows)-1].ID, 10)
	}
	for _, r := range rows {
		res.Problems = append(res.Problems, &v1.ProblemSummary{
			Slug:  r.Slug,
			Title: r.Title,
		})
	}
	return connect.NewResponse(res), nil
}

func (s *Service) GetProblem(ctx context.Context, req *connect.Request[v1.GetProblemRequest]) (*connect.Response[v1.GetProblemResponse], error) {
	// One snapshot for both reads, so a concurrent import can't pair the
	// old statement and limits with the new samples.
	var (
		p     db.Problem
		tests []db.Test
	)
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly},
		func(tx pgx.Tx) error {
			q := db.New(tx)
			var err error
			if p, err = q.GetProblemBySlug(ctx, req.Msg.Slug); err != nil {
				return err
			}
			if p.Visibility != db.ProblemVisibilityPublic && !canSeePrivate(ctx) {
				return pgx.ErrNoRows // a private problem doesn't exist to contestants
			}
			tests, err = q.ListSampleTests(ctx, p.ID)
			return err
		})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("problem %q not found", req.Msg.Slug))
	}
	if err != nil {
		return nil, internal(ctx, err)
	}
	samples := make([]*v1.SampleTest, len(tests))
	for i, t := range tests {
		in, err := s.read(ctx, t.InputHash)
		if err != nil {
			return nil, internal(ctx, err)
		}
		out, err := s.read(ctx, t.OutputHash)
		if err != nil {
			return nil, internal(ctx, err)
		}
		samples[i] = &v1.SampleTest{Input: in, Output: out}
	}

	return connect.NewResponse(&v1.GetProblemResponse{Problem: &v1.Problem{
		Slug:           p.Slug,
		Title:          p.Title,
		StatementMd:    p.StatementMd,
		TimeLimitMs:    p.TimeLimitMs,
		MemoryLimitKib: p.MemoryLimitKib,
		Samples:        samples,
	}}), nil
}

func (s *Service) read(ctx context.Context, hash string) (string, error) {
	r, err := s.blobs.Get(ctx, blob.Key(hash))
	if err != nil {
		return "", fmt.Errorf("read blob %s: %w", hash, err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	return string(b), err
}

func internal(ctx context.Context, err error) error {
	slog.ErrorContext(ctx, "problems: internal error", "err", err)
	return connect.NewError(connect.CodeInternal, errors.New("internal error"))
}
