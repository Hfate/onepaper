package crawler

import (
	"context"
	"fmt"

	"github.com/Hfate/onepaper/internal/model"
)

// NatureStub 占位：仅 metadata，后续可接 Crossref / 官方 API。
type NatureStub struct{}

func (NatureStub) Name() string { return "nature" }

func (NatureStub) FetchRecent(ctx context.Context, limit int) ([]model.Paper, error) {
	_ = ctx
	_ = limit
	return nil, fmt.Errorf("nature crawler not implemented: register Crossref or Nature API")
}

// ScienceStub 占位。
type ScienceStub struct{}

func (ScienceStub) Name() string { return "science" }

func (ScienceStub) FetchRecent(ctx context.Context, limit int) ([]model.Paper, error) {
	_ = ctx
	_ = limit
	return nil, fmt.Errorf("science crawler not implemented")
}

// LancetStub 占位。
type LancetStub struct{}

func (LancetStub) Name() string { return "lancet" }

func (LancetStub) FetchRecent(ctx context.Context, limit int) ([]model.Paper, error) {
	_ = ctx
	_ = limit
	return nil, fmt.Errorf("lancet crawler not implemented")
}
