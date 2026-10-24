package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	streamlib "github.com/djherbis/stream"
	"github.com/gofrs/flock"
	"github.com/hashicorp/golang-lru/v2/simplelru"
)

var errDiskCacheClosed = errors.New("object disk cache is closed")
var errDiskCacheFillUnavailable = errors.New("object disk cache fill unavailable")

const (
	diskCacheMarkerName  = ".regimux-object-cache"
	diskCacheMarkerValue = "regimux-object-cache-v1\n"
)

type DiskCacheOptions struct {
	Directory          string
	MaxBytes           int64
	MaxObjectBytes     int64
	MaxEntries         int
	MaxConcurrentFills int
	TTL                time.Duration
	Logger             *slog.Logger
	Observe            func(context.Context, DiskCacheEvent)
}

type DiskCacheEvent struct {
	Operation string
	Result    string
	Bytes     int64
}

type DiskCacheStats struct {
	Bytes         int64
	ReservedBytes int64
	Entries       int
	Fills         int
}

type diskCacheMetadata struct {
	Digest      string `json:"digest"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type,omitempty"`
	ETag        string `json:"etag,omitempty"`
	StoredAt    int64  `json:"stored_at"`
	LastAccess  int64  `json:"last_access"`
	FileModTime int64  `json:"file_mod_time"`
}

type diskCacheEntry struct {
	metadata     diskCacheMetadata
	bodyPath     string
	metaPath     string
	lastAccess   time.Time
	pins         int
	evictPending bool
}

type diskCacheFill struct {
	digest          string
	info            Info
	tempPath        string
	reserve         int64
	invalid         bool
	keyToken        *diskCacheKeyToken
	ctx             context.Context
	cancel          context.CancelFunc
	source          io.ReadCloser
	sourceCloseOnce sync.Once
	sourceCloseErr  error
	stream          *streamlib.Stream
	ready           chan struct{}
	readyErr        error
	readyClosed     bool
	refsDone        chan struct{}
	refsDoneOnce    sync.Once
	directReader    io.ReadCloser
	directInfo      *Info
	refs            int
	fullConsumed    bool
	producerDone    bool
	producerErr     error
	finalizing      bool
	done            chan struct{}
	ownerActive     bool
	bypass          bool
}

// DiskCacheStore is a bounded local read-through cache for a canonical object store.
type DiskCacheStore struct {
	mu               sync.Mutex
	rootMu           sync.RWMutex
	backend          Store
	opts             DiskCacheOptions
	root             string
	objectsDir       string
	tempDir          string
	rootFS           *os.Root
	lock             *flock.Flock
	entries          map[string]*diskCacheEntry
	lru              *simplelru.LRU[string, *diskCacheEntry]
	fills            map[string]*diskCacheFill
	activeFlights    map[*diskCacheFill]struct{}
	keyStates        map[string]*diskCacheKeyState
	readers          map[*diskCacheFileReader]struct{}
	flightReaders    map[*diskCacheFlightReader]struct{}
	directTracker    *diskCacheDirectTracker
	bytes            int64
	reserved         int64
	orphanedBytes    int64
	diskFillDisabled bool
	closed           bool
	lockReleased     bool
	closeOnce        sync.Once
	closeErr         error
}

// NewDiskCache constructs an exclusive, persistent disk cache directory.
func NewDiskCache(backend Store, opts DiskCacheOptions) (*DiskCacheStore, error) {
	if backend == nil {
		return nil, errors.New("disk cache object store is required")
	}
	if err := validateDiskCacheOptions(opts); err != nil {
		return nil, err
	}
	root, err := prepareDiskCacheDirectory(opts.Directory)
	if err != nil {
		return nil, err
	}
	lock, err := lockDiskCacheDirectory(root)
	if err != nil {
		return nil, err
	}
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open disk cache root: %w", err), lock.Unlock())
	}
	lru, err := simplelru.NewLRU[string, *diskCacheEntry](opts.MaxEntries, nil)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("create disk cache index: %w", err),
			wrapDiskCacheClose("close disk cache root", rootFS.Close()),
			wrapDiskCacheClose("release disk cache lock", lock.Unlock()),
		)
	}
	cache := &DiskCacheStore{
		backend:       backend,
		opts:          opts,
		root:          root,
		objectsDir:    filepath.Join(root, "objects"),
		tempDir:       filepath.Join(root, "temporary"),
		rootFS:        rootFS,
		lock:          lock,
		entries:       make(map[string]*diskCacheEntry),
		lru:           lru,
		fills:         make(map[string]*diskCacheFill),
		activeFlights: make(map[*diskCacheFill]struct{}),
		keyStates:     make(map[string]*diskCacheKeyState),
		readers:       make(map[*diskCacheFileReader]struct{}),
		flightReaders: make(map[*diskCacheFlightReader]struct{}),
		directTracker: newDiskCacheDirectTracker(),
	}
	if err := cache.initializeDirectory(); err != nil {
		return nil, errors.Join(err,
			wrapDiskCacheClose("close disk cache root", rootFS.Close()),
			wrapDiskCacheClose("release disk cache lock", lock.Unlock()),
		)
	}
	if cache.opts.Logger == nil {
		cache.opts.Logger = slog.Default()
	}
	return cache, nil
}

func validateDiskCacheOptions(opts DiskCacheOptions) error {
	if opts.Directory == "" {
		return errors.New("disk cache directory is required")
	}
	if opts.MaxBytes <= 0 || opts.MaxObjectBytes <= 0 || opts.MaxEntries <= 0 || opts.MaxConcurrentFills <= 0 {
		return errors.New("disk cache byte, entry, and fill limits must be positive")
	}
	if opts.MaxObjectBytes > opts.MaxBytes {
		return errors.New("disk cache object limit cannot exceed total byte limit")
	}
	if opts.TTL <= 0 {
		return errors.New("disk cache TTL must be positive")
	}
	return nil
}
