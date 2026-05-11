package jobs

import "fmt"

// State identifies a durable step in the vdb-guardian job lifecycle. Job states
// are intentionally explicit so future runners can checkpoint, resume, retry,
// and report long-running vector database verification work.
//
// State 标识了 vdb-guardian 作业生命周期中的持久化流转步骤。
// 将作业状态设计得如此明确，是为了让未来的运行器能够对那些耗时极长的
// 向量数据库验证工作执行断点检查 (checkpoint)、恢复 (resume)、重试 (retry) 及状态报告。
type State string

const (
	// StateCreated marks a job that has been accepted but not validated yet.
	//
	// StateCreated 标记该作业已被接受，但尚未进行参数合法性校验。
	StateCreated State = "CREATED"
	// StateValidatingConfig marks a job whose declarative configuration is being checked.
	//
	// StateValidatingConfig 标记该作业正在进行声明式配置的校验工作。
	StateValidatingConfig State = "VALIDATING_CONFIG"
	// StateConnectingSource marks a job that is opening the source vector database connection.
	//
	// StateConnectingSource 标记该作业正在开启至源端向量数据库的连接。
	StateConnectingSource State = "CONNECTING_SOURCE"
	// StateConnectingTarget marks a job that is opening the target vector database connection.
	//
	// StateConnectingTarget 标记该作业正在开启至目标端向量数据库的连接。
	StateConnectingTarget State = "CONNECTING_TARGET"
	// StateSamplingQueries marks a job that is preparing or loading verification query samples.
	//
	// StateSamplingQueries 标记该作业正在准备或加载验证查询数据的样本。
	StateSamplingQueries State = "SAMPLING_QUERIES"
	// StateCollectingSourceResults marks a job that is collecting source-side search results.
	//
	// StateCollectingSourceResults 标记该作业正在收集源端检索结果。
	StateCollectingSourceResults State = "COLLECTING_SOURCE_RESULTS"
	// StateCollectingTargetResults marks a job that is collecting target-side search results.
	//
	// StateCollectingTargetResults 标记该作业正在收集目标端检索结果。
	StateCollectingTargetResults State = "COLLECTING_TARGET_RESULTS"
	// StateRunningFingerprintEngine marks a job that is comparing retrieval behavior fingerprints.
	//
	// StateRunningFingerprintEngine 标记该作业正在比对检索行为指纹。
	StateRunningFingerprintEngine State = "RUNNING_FINGERPRINT_ENGINE"
	// StateGeneratingReport marks a job that is rendering JSON, Markdown, or future HTML reports.
	//
	// StateGeneratingReport 标记该作业正在渲染输出 JSON、Markdown 或未来的 HTML 格式报告。
	StateGeneratingReport State = "GENERATING_REPORT"
	// StateSucceeded marks a job that completed all verification steps successfully.
	//
	// StateSucceeded 标记该作业已成功完成所有验证步骤。
	StateSucceeded State = "SUCCEEDED"
	// StateFailed marks a job that stopped because an unrecoverable error occurred.
	//
	// StateFailed 标记该作业因为遇到了不可恢复的致命错误而被迫终止。
	StateFailed State = "FAILED"
	// StateCancelled marks a job that was intentionally stopped by an operator or caller.
	//
	// StateCancelled 标记该作业被操作人员或外部调用方人为主动终止。
	StateCancelled State = "CANCELLED"
)

// String returns the wire-format name of the state. It is used by logs, reports,
// configuration snapshots, and future API responses so state names remain stable
// across Go string formatting contexts.
//
// String 返回该状态的序列化名称 (wire-format name)。
// 该名称被广泛应用于日志、报告、配置快照以及未来的 API 响应中，
// 从而确保在 Go 语言的各种字符串格式化上下文中，状态名称始终保持稳定一致。
func (s State) String() string {
	return string(s)
}

// IsTerminal reports whether the state represents the end of normal job
// progression. Terminal states are important for runners because they must not
// be retried or advanced without an explicit new operator action.
//
// IsTerminal 报告该状态是否代表作业正常流转的终点。
// 终态 (Terminal states) 对于运行器而言极其重要，因为除非操作人员发起明确的全新操作指令，
// 否则系统绝对不允许对处于终态的作业执行自动重试或继续推进。
func (s State) IsTerminal() bool {
	switch s {
	case StateSucceeded, StateFailed, StateCancelled:
		return true
	default:
		return false
	}
}

// ParseState converts a wire-format state name into a typed State. It returns an
// error for unknown values so API, CLI, and configuration callers can fail fast
// instead of silently accepting invalid lifecycle states.
//
// ParseState 将序列化的状态名称解析为强类型的 State 枚举。
// 当遇到未知的状态值时，它会果断抛出错误。这种“快速失败 (fail fast)”的设计，
// 确保了 API、CLI 和配置解析模块不会在静默中吞下非法的生命周期状态。
func ParseState(value string) (State, error) {
	state := State(value)
	switch state {
	case StateCreated,
		StateValidatingConfig,
		StateConnectingSource,
		StateConnectingTarget,
		StateSamplingQueries,
		StateCollectingSourceResults,
		StateCollectingTargetResults,
		StateRunningFingerprintEngine,
		StateGeneratingReport,
		StateSucceeded,
		StateFailed,
		StateCancelled:
		return state, nil
	default:
		return "", fmt.Errorf("unknown job state %q", value)
	}
}
