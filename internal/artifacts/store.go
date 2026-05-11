package artifacts

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var errEmptyArtifactPath = errors.New("artifact path must not be empty")

// Store defines the storage boundary for generated fingerprints, comparison
// inputs, reports, and other job artifacts. Production implementations can write
// to local disk, S3, MinIO, or other object stores while callers keep the same
// interface.
//
// Store 定义了产物存储的边界。用于存放生成的指纹、比对输入参数、最终报告以及
// 其他与验证作业相关的中间产物。生产环境的实现可以将数据写入本地磁盘、S3、MinIO
// 或是其他对象存储，而对于调用方而言，它们所依赖的接口始终保持不变。
type Store interface {
	// Put stores data at path, replacing any previous value for that path.
	//
	// Put 将数据存储到指定路径下。如果该路径下已存在旧值，将被覆盖。
	Put(ctx context.Context, path string, data []byte) error
	// Get returns a copy of the data stored at path or an error when the artifact is missing.
	//
	// Get 返回存储在指定路径下数据的拷贝。当产物缺失时，将返回错误。
	Get(ctx context.Context, path string) ([]byte, error)
	// Exists reports whether an artifact path has been stored.
	//
	// Exists 报告指定路径的产物是否已被存储。
	Exists(ctx context.Context, path string) (bool, error)
}

// MemoryStore is an in-memory Store implementation used by unit tests and early
// scaffolding. It copies data on write and read so callers cannot mutate internal
// state through shared byte slices.
//
// MemoryStore 是用于单元测试和早期脚手架搭建的纯内存 Store 实现。
// 它在写入和读取时都会主动执行数据拷贝，从而从根本上杜绝了调用方通过
// 共享的字节切片 (byte slices) 意外篡改内部状态的可能性。
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMemoryStore creates an empty in-memory artifact store. The store is safe for
// concurrent use by tests or lightweight local runners because all access is
// protected by a read-write mutex.
//
// NewMemoryStore 创建一个空的内存型产物存储。
// 由于所有的读写访问均被读写互斥锁 (read-write mutex) 所保护，
// 因此该存储在测试用例或轻量级本地运行器的并发调用下是绝对安全的。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string][]byte)}
}

// Put stores a copy of data under path. It returns an error for empty paths so
// job runners do not accidentally write artifacts that cannot be addressed later.
//
// Put 会在指定路径下存储数据的拷贝。对于空路径它将直接抛出错误，
// 这样可以防止作业运行器意外写入那些在后续环节无法被寻址访问的“孤儿产物”。
func (s *MemoryStore) Put(ctx context.Context, path string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if path == "" {
		return errEmptyArtifactPath
	}

	copyData := append([]byte(nil), data...)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[path] = copyData
	return nil
}

// Get returns a copy of the artifact stored at path. A missing path returns an
// error with the requested path so callers can include useful diagnostic context
// in job failure reports.
//
// Get 返回存储在指定路径下产物的拷贝。当路径缺失时，返回的错误信息中将包含
// 该请求路径，以便调用方能够在作业失败报告中附带更具价值的诊断上下文。
func (s *MemoryStore) Get(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, errEmptyArtifactPath
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.data[path]
	if !ok {
		return nil, fmt.Errorf("artifact %q not found", path)
	}
	return append([]byte(nil), value...), nil
}

// Exists reports whether path is present in the store. It validates empty paths
// consistently with Put and Get so all artifact operations share the same input
// contract.
//
// Exists 报告指定路径是否存在于存储中。
// 它与 Put 和 Get 保持一致的空路径强校验逻辑，确保所有的产物操作
// 共享着绝对相同的输入契约。
func (s *MemoryStore) Exists(ctx context.Context, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if path == "" {
		return false, errEmptyArtifactPath
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[path]
	return ok, nil
}
