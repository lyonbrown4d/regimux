package cache

import (
	"github.com/lyonbrown4d/regimux/internal/cachegc"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type CleanupService = cachegc.Service
type CleanupOptions = cachegc.CleanupOptions
type CleanupReport = cachegc.CleanupReport
type ReconcileOptions = cachegc.ReconcileOptions
type ReconcileReport = cachegc.ReconcileReport

func NewCleanupService(metadata meta.Store, objects object.Store) *CleanupService {
	return cachegc.NewCleanupService(metadata, objects)
}
