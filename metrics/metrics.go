package metrics

import (
	"context"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	rpcmetrics "github.com/filecoin-project/go-jsonrpc/metrics"
	_ "github.com/influxdata/influxdb1-client"

	"github.com/EpiK-Protocol/go-epik/blockstore"
)

// Distribution
var defaultMillisecondsDistribution = view.Distribution(0.01, 0.05, 0.1, 0.3, 0.6, 0.8, 1, 2, 3, 4, 5, 6, 8, 10, 13, 16, 20, 25, 30, 40, 50, 65, 80, 100, 130, 160, 200, 250, 300, 400, 500, 650, 800, 1000, 2000, 3000, 4000, 5000, 7500, 10000, 20000, 50000, 100000)
var workMillisecondsDistribution = view.Distribution(
	250, 500, 1000, 2000, 5000, 10_000, 30_000, 60_000, 2*60_000, 5*60_000, 10*60_000, 15*60_000, 30*60_000, // short sealing tasks
	40*60_000, 45*60_000, 50*60_000, 55*60_000, 60*60_000, 65*60_000, 70*60_000, 75*60_000, 80*60_000, 85*60_000, 100*60_000, 120*60_000, // PC2 / C2 range
	130*60_000, 140*60_000, 150*60_000, 160*60_000, 180*60_000, 200*60_000, 220*60_000, 260*60_000, 300*60_000, // PC1 range
	350*60_000, 400*60_000, 600*60_000, 800*60_000, 1000*60_000, 1300*60_000, 1800*60_000, 4000*60_000, 10000*60_000, // intel PC1 range
)

// Global Tags
var (
	// common
	Version, _     = tag.NewKey("version")
	Commit, _      = tag.NewKey("commit")
	NodeType, _    = tag.NewKey("node_type")
	PeerID, _      = tag.NewKey("peer_id")
	MinerID, _     = tag.NewKey("miner_id")
	Coinbase, _    = tag.NewKey("coinbase")
	FailureType, _ = tag.NewKey("failure_type")

	// chain
	Local, _        = tag.NewKey("local")
	MessageFrom, _  = tag.NewKey("message_from")
	MessageTo, _    = tag.NewKey("message_to")
	MessageNonce, _ = tag.NewKey("message_nonce")
	ReceivedFrom, _ = tag.NewKey("received_from")
	Endpoint, _     = tag.NewKey("endpoint")
	APIInterface, _ = tag.NewKey("api") // to distinguish between gateway api and full node api endpoint calls

	Type, _ = tag.NewKey("type")
	// miner
	TaskType, _       = tag.NewKey("task_type")
	WorkerHostname, _ = tag.NewKey("worker_hostname")
)

// Measures
var (
	// common
	EpikInfo           = stats.Int64("info", "Arbitrary counter to tag epik info to", stats.UnitDimensionless)
	PeerCount          = stats.Int64("peer/count", "Current number of EpiK peers", stats.UnitDimensionless)
	APIRequestDuration = stats.Float64("api/request_duration_ms", "Duration of API requests", stats.UnitMilliseconds)

	// chain
	ChainNodeHeight                     = stats.Int64("chain/node_height", "Current Height of the node", stats.UnitDimensionless)
	ChainNodeHeightExpected             = stats.Int64("chain/node_height_expected", "Expected Height of the node", stats.UnitDimensionless)
	ChainNodeWorkerHeight               = stats.Int64("chain/node_worker_height", "Current Height of workers on the node", stats.UnitDimensionless)
	MessagePublished                    = stats.Int64("message/published", "Counter for total locally published messages", stats.UnitDimensionless)
	MessageReceived                     = stats.Int64("message/received", "Counter for total received messages", stats.UnitDimensionless)
	MessageValidationFailure            = stats.Int64("message/failure", "Counter for message validation failures", stats.UnitDimensionless)
	MessageValidationSuccess            = stats.Int64("message/success", "Counter for message validation successes", stats.UnitDimensionless)
	BlockPublished                      = stats.Int64("block/published", "Counter for total locally published blocks", stats.UnitDimensionless)
	BlockReceived                       = stats.Int64("block/received", "Counter for total received blocks", stats.UnitDimensionless)
	BlockValidationFailure              = stats.Int64("block/failure", "Counter for block validation failures", stats.UnitDimensionless)
	BlockValidationSuccess              = stats.Int64("block/success", "Counter for block validation successes", stats.UnitDimensionless)
	BlockValidationDurationMilliseconds = stats.Float64("block/validation_ms", "Duration for Block Validation in ms", stats.UnitMilliseconds)
	BlockDelay                          = stats.Int64("block/delay", "Delay of accepted blocks, where delay is >5s", stats.UnitMilliseconds)
	PubsubPublishMessage                = stats.Int64("pubsub/published", "Counter for total published messages", stats.UnitDimensionless)
	PubsubDeliverMessage                = stats.Int64("pubsub/delivered", "Counter for total delivered messages", stats.UnitDimensionless)
	PubsubRejectMessage                 = stats.Int64("pubsub/rejected", "Counter for total rejected messages", stats.UnitDimensionless)
	PubsubDuplicateMessage              = stats.Int64("pubsub/duplicate", "Counter for total duplicate messages", stats.UnitDimensionless)
	PubsubRecvRPC                       = stats.Int64("pubsub/recv_rpc", "Counter for total received RPCs", stats.UnitDimensionless)
	PubsubSendRPC                       = stats.Int64("pubsub/send_rpc", "Counter for total sent RPCs", stats.UnitDimensionless)
	PubsubDropRPC                       = stats.Int64("pubsub/drop_rpc", "Counter for total dropped RPCs", stats.UnitDimensionless)
	VMFlushCopyDuration                 = stats.Float64("vm/flush_copy_ms", "Time spent in VM Flush Copy", stats.UnitMilliseconds)
	VMFlushCopyCount                    = stats.Int64("vm/flush_copy_count", "Number of copied objects", stats.UnitDimensionless)

	MessageReceivedBytes    = stats.Int64("message/received_bytes", "Counter for total bytes of received messages", stats.UnitBytes)
	BlockReceivedBytes      = stats.Int64("block/received_bytes", "Counter for total bytes of received blocks", stats.UnitBytes)
	ServeSyncSuccess        = stats.Int64("serve/sync_success", "Counter for successes", stats.UnitDimensionless)
	ServeSyncFailure        = stats.Int64("serve/sync_failure", "Counter for failures", stats.UnitDimensionless)
	ServeSyncBytes          = stats.Int64("serve/sync_bytes", "Counter for total sent bytes", stats.UnitBytes)
	TipsetMessagesCount     = stats.Int64("tipset/messages_count", "Counter of messages in tipsets", stats.UnitDimensionless)
	TipsetMessagesRate      = stats.Float64("tipset/messages_rate", "Counter of processed messages per second", stats.UnitDimensionless)
	TipsetPublishDealsCount = stats.Int64("tipset/publishdeals_count", "Counter of publishdeals in tipsets", stats.UnitDimensionless)
	TipsetSubmitPoStsCount  = stats.Int64("tipset/submitposts_count", "Counter of submitposts in tipsets", stats.UnitDimensionless)
	TipsetGasUsed           = stats.Int64("tipset/gasused", "Counter of gas in one tipset", stats.UnitDimensionless)
	TipsetGasReward         = stats.Int64("tipset/gasreward", "Counter of gas reward in one tipset", stats.UnitDimensionless)
	TipsetGasPenalty        = stats.Int64("tipset/gaspenalty", "Counter of gas penalty in one tipset", stats.UnitDimensionless)
	TipsetGasBurn           = stats.Int64("tipset/gasburn", "Counter of gas burn in one tipset", stats.UnitDimensionless)

	MessageRevert      = stats.Int64("message/revert", "Counter for total revert messages", stats.UnitDimensionless)
	MessageRevertBytes = stats.Int64("message/revert_bytes", "Counter for total bytes of revert messages", stats.UnitBytes)
	BlockRevert        = stats.Int64("block/revert", "Counter for total revert blocks", stats.UnitDimensionless)
	BlockRevertBytes   = stats.Int64("block/revert_bytes", "Counter for total bytes of revert blocks", stats.UnitBytes)

	// Sys
	BandwidthTotal = stats.Int64("bandwidth/total", "Counter for total traffic bytes", stats.UnitBytes)
	BandwidthRate  = stats.Float64("bandwidth/rate", "Counter for bytes per second", stats.UnitBytes)
	SysCpuUsed     = stats.Int64("sys/cpu_used", "Counter for used percentage of cpu used", stats.UnitDimensionless)
	SysMemUsed     = stats.Int64("sys/mem_used", "Counter for used percentage of ram used", stats.UnitDimensionless)
	SysDiskUsed    = stats.Int64("sys/disk_used", "Counter for used percentage of disk", stats.UnitDimensionless)

	// miner
	WorkerCallsStarted           = stats.Int64("sealing/worker_calls_started", "Counter of started worker tasks", stats.UnitDimensionless)
	WorkerCallsReturnedCount     = stats.Int64("sealing/worker_calls_returned_count", "Counter of returned worker tasks", stats.UnitDimensionless)
	WorkerCallsReturnedDuration  = stats.Float64("sealing/worker_calls_returned_ms", "Counter of returned worker tasks", stats.UnitMilliseconds)
	WorkerUntrackedCallsReturned = stats.Int64("sealing/worker_untracked_calls_returned", "Counter of returned untracked worker tasks", stats.UnitDimensionless)

	ServeTransferBytes  = stats.Int64("serve/transfer_bytes", "Counter for total sent bytes", stats.UnitBytes)
	ServeTransferAccept = stats.Int64("serve/transfer_accept", "Counter for total accepted requests", stats.UnitDimensionless)
	ServeTransferResult = stats.Int64("serve/transfer_result", "Counter for process results", stats.UnitDimensionless)
	CoinbaseBalance     = stats.Float64("coinbase/balance", "Counter for coinbase balance in EPK", stats.UnitDimensionless)
	MinerPower          = stats.Int64("miner/power", "miner power", stats.UnitBytes)
	MinerSectorCount    = stats.Int64("miner/sector", "Counter for miner sector with type", stats.UnitDimensionless)

	// splitstore
	SplitstoreMiss                  = stats.Int64("splitstore/miss", "Number of misses in hotstre access", stats.UnitDimensionless)
	SplitstoreCompactionTimeSeconds = stats.Float64("splitstore/compaction_time", "Compaction time in seconds", stats.UnitSeconds)
	SplitstoreCompactionHot         = stats.Int64("splitstore/hot", "Number of hot blocks in last compaction", stats.UnitDimensionless)
	SplitstoreCompactionCold        = stats.Int64("splitstore/cold", "Number of cold blocks in last compaction", stats.UnitDimensionless)
	SplitstoreCompactionDead        = stats.Int64("splitstore/dead", "Number of dead blocks in last compaction", stats.UnitDimensionless)

	SplitstoreBytes     = stats.Int64("splitstore/bytes", "Counter for total bytes of storage", stats.UnitBytes)
	SplitstoreColdBytes = stats.Int64("splitstore/cold_bytes", "Counter for total bytes of cold storage", stats.UnitBytes)

	// mpool
	MpoolPendingCount = stats.Int64("mpool/pending_count", "Counter of pending messages in mpool", stats.UnitDimensionless)
)

var (
	InfoView = &view.View{
		Name:        "info",
		Description: "epik node information",
		Measure:     EpikInfo,
		Aggregation: view.LastValue(),
		TagKeys:     []tag.Key{Version, Commit},
	}
	ChainNodeHeightView = &view.View{
		Measure:     ChainNodeHeight,
		Aggregation: view.LastValue(),
	}
	ChainNodeHeightExpectedView = &view.View{
		Measure:     ChainNodeHeightExpected,
		Aggregation: view.LastValue(),
	}
	ChainNodeWorkerHeightView = &view.View{
		Measure:     ChainNodeWorkerHeight,
		Aggregation: view.LastValue(),
	}
	BlockReceivedView = &view.View{
		Measure:     BlockReceived,
		Aggregation: view.Count(),
	}
	BlockValidationFailureView = &view.View{
		Measure:     BlockValidationFailure,
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{FailureType},
	}
	BlockValidationSuccessView = &view.View{
		Measure:     BlockValidationSuccess,
		Aggregation: view.Count(),
	}
	BlockValidationDurationView = &view.View{
		Measure:     BlockValidationDurationMilliseconds,
		Aggregation: defaultMillisecondsDistribution,
	}
	BlockDelayView = &view.View{
		Measure: BlockDelay,
		TagKeys: []tag.Key{MinerID},
		Aggregation: func() *view.Aggregation {
			var bounds []float64
			for i := 5; i < 29; i++ { // 5-29s, step 1s
				bounds = append(bounds, float64(i*1000))
			}
			for i := 30; i < 60; i += 2 { // 30-58s, step 2s
				bounds = append(bounds, float64(i*1000))
			}
			for i := 60; i <= 300; i += 10 { // 60-300s, step 10s
				bounds = append(bounds, float64(i*1000))
			}
			bounds = append(bounds, 600*1000) // final cutoff at 10m
			return view.Distribution(bounds...)
		}(),
	}
	MessagePublishedView = &view.View{
		Measure:     MessagePublished,
		Aggregation: view.Count(),
	}
	MessageReceivedView = &view.View{
		Measure:     MessageReceived,
		Aggregation: view.Count(),
	}
	MessageValidationFailureView = &view.View{
		Measure:     MessageValidationFailure,
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{FailureType, Local},
	}
	MessageValidationSuccessView = &view.View{
		Measure:     MessageValidationSuccess,
		Aggregation: view.Count(),
	}
	PeerCountView = &view.View{
		Measure:     PeerCount,
		Aggregation: view.LastValue(),
	}
	PubsubPublishMessageView = &view.View{
		Measure:     PubsubPublishMessage,
		Aggregation: view.Count(),
	}
	PubsubDeliverMessageView = &view.View{
		Measure:     PubsubDeliverMessage,
		Aggregation: view.Count(),
	}
	PubsubRejectMessageView = &view.View{
		Measure:     PubsubRejectMessage,
		Aggregation: view.Count(),
	}
	PubsubDuplicateMessageView = &view.View{
		Measure:     PubsubDuplicateMessage,
		Aggregation: view.Count(),
	}
	PubsubRecvRPCView = &view.View{
		Measure:     PubsubRecvRPC,
		Aggregation: view.Count(),
	}
	PubsubSendRPCView = &view.View{
		Measure:     PubsubSendRPC,
		Aggregation: view.Count(),
	}
	PubsubDropRPCView = &view.View{
		Measure:     PubsubDropRPC,
		Aggregation: view.Count(),
	}
	APIRequestDurationView = &view.View{
		Measure:     APIRequestDuration,
		Aggregation: defaultMillisecondsDistribution,
		TagKeys:     []tag.Key{APIInterface, Endpoint},
	}
	VMFlushCopyDurationView = &view.View{
		Measure:     VMFlushCopyDuration,
		Aggregation: view.Sum(),
	}
	VMFlushCopyCountView = &view.View{
		Measure:     VMFlushCopyCount,
		Aggregation: view.Sum(),
	}

	MessageReceivedBytesView = &view.View{
		Measure:     MessageReceivedBytes,
		Aggregation: view.Sum(),
	}
	BlockReceivedBytesView = &view.View{
		Measure:     BlockReceivedBytes,
		Aggregation: view.Sum(),
	}
	ServeSyncSuccessView = &view.View{
		Measure:     ServeSyncSuccess,
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{Type},
	}
	ServeSyncFailureView = &view.View{
		Measure:     ServeSyncFailure,
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{FailureType},
	}
	ServeSyncBytesView = &view.View{
		Measure:     ServeSyncBytes,
		Aggregation: view.Sum(),
		TagKeys:     []tag.Key{Type},
	}
	TipsetMessagesCountView = &view.View{
		Measure:     TipsetMessagesCount,
		Aggregation: view.LastValue(),
	}
	TipsetMessagesRateView = &view.View{
		Measure:     TipsetMessagesRate,
		Aggregation: view.LastValue(),
	}
	TipsetPublishDealsCountView = &view.View{
		Measure:     TipsetPublishDealsCount,
		Aggregation: view.LastValue(),
	}
	TipsetSubmitPoStsCountView = &view.View{
		Measure:     TipsetSubmitPoStsCount,
		Aggregation: view.LastValue(),
	}
	TipsetGasUsedView = &view.View{
		Measure:     TipsetGasUsed,
		Aggregation: view.LastValue(),
	}
	TipsetGasRewardView = &view.View{
		Measure:     TipsetGasReward,
		Aggregation: view.Sum(),
	}
	TipsetGasPenaltyView = &view.View{
		Measure:     TipsetGasPenalty,
		Aggregation: view.Sum(),
	}
	TipsetGasBurnView = &view.View{
		Measure:     TipsetGasBurn,
		Aggregation: view.Sum(),
	}

	MessageRevertView = &view.View{
		Measure:     MessageRevert,
		Aggregation: view.Count(),
	}

	MessageRevertBytesView = &view.View{
		Measure:     MessageRevertBytes,
		Aggregation: view.Sum(),
	}

	BlockRevertView = &view.View{
		Measure:     BlockRevert,
		Aggregation: view.Count(),
	}

	BlockRevertBytesView = &view.View{
		Measure:     BlockRevertBytes,
		Aggregation: view.Sum(),
	}

	// default
	BandwidthTotalView = &view.View{
		Measure:     BandwidthTotal,
		Aggregation: view.LastValue(),
		TagKeys:     []tag.Key{Type, NodeType},
	}
	BandwidthRateView = &view.View{
		Measure:     BandwidthRate,
		Aggregation: view.LastValue(),
		TagKeys:     []tag.Key{Type, NodeType},
	}
	SysCpuUsedView = &view.View{
		Measure:     SysCpuUsed,
		Aggregation: view.LastValue(),
		TagKeys:     []tag.Key{NodeType},
	}
	SysMemUsedView = &view.View{
		Measure:     SysMemUsed,
		Aggregation: view.LastValue(),
		TagKeys:     []tag.Key{NodeType},
	}
	SysDiskUsedView = &view.View{
		Measure:     SysDiskUsed,
		Aggregation: view.LastValue(),
		TagKeys:     []tag.Key{NodeType},
	}

	// miner
	WorkerCallsStartedView = &view.View{
		Measure:     WorkerCallsStarted,
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{TaskType, WorkerHostname},
	}
	WorkerCallsReturnedCountView = &view.View{
		Measure:     WorkerCallsReturnedCount,
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{TaskType, WorkerHostname},
	}
	WorkerUntrackedCallsReturnedView = &view.View{
		Measure:     WorkerUntrackedCallsReturned,
		Aggregation: view.Count(),
	}
	WorkerCallsReturnedDurationView = &view.View{
		Measure:     WorkerCallsReturnedDuration,
		Aggregation: workMillisecondsDistribution,
		TagKeys:     []tag.Key{TaskType, WorkerHostname},
	}

	ServeTransferBytesView = &view.View{
		Measure:     ServeTransferBytes,
		Aggregation: view.Sum(),
		TagKeys:     []tag.Key{Type},
	}
	ServeTransferAcceptView = &view.View{
		Measure:     ServeTransferAccept,
		Aggregation: view.Count(),
	}
	ServeTransferResultView = &view.View{
		Measure:     ServeTransferResult,
		Aggregation: view.Count(),
		TagKeys:     []tag.Key{Type},
	}
	CoinbaseBalanceView = &view.View{
		Measure:     CoinbaseBalance,
		Aggregation: view.LastValue(),
		TagKeys:     []tag.Key{Type, Coinbase},
	}
	MinerPowerView = &view.View{
		Measure:     MinerPower,
		Aggregation: view.LastValue(),
		TagKeys:     []tag.Key{Type, MinerID},
	}
	MinerSectorView = &view.View{
		Measure:     MinerSectorCount,
		Aggregation: view.LastValue(),
		TagKeys:     []tag.Key{Type, MinerID},
	}

	// splitstore
	SplitstoreMissView = &view.View{
		Measure:     SplitstoreMiss,
		Aggregation: view.Count(),
	}
	SplitstoreCompactionTimeSecondsView = &view.View{
		Measure:     SplitstoreCompactionTimeSeconds,
		Aggregation: view.LastValue(),
	}
	SplitstoreCompactionHotView = &view.View{
		Measure:     SplitstoreCompactionHot,
		Aggregation: view.LastValue(),
	}
	SplitstoreCompactionColdView = &view.View{
		Measure:     SplitstoreCompactionCold,
		Aggregation: view.Sum(),
	}
	SplitstoreCompactionDeadView = &view.View{
		Measure:     SplitstoreCompactionDead,
		Aggregation: view.Sum(),
	}
	SplitstoreBytesView = &view.View{
		Measure:     SplitstoreBytes,
		Aggregation: view.Sum(),
	}
	SplitstoreColdBytesView = &view.View{
		Measure:     SplitstoreColdBytes,
		Aggregation: view.Sum(),
	}
	MpoolPendingCountView = &view.View{
		Measure:     MpoolPendingCount,
		Aggregation: view.LastValue(),
	}
)

// DefaultViews is an array of OpenCensus views for metric gathering purposes
var DefaultViews = func() []*view.View {
	views := []*view.View{
		InfoView,
		PeerCountView,
		APIRequestDurationView,

		BandwidthTotalView,
		BandwidthRateView,
		SysCpuUsedView,
		SysMemUsedView,
		SysDiskUsedView,
	}
	views = append(views, blockstore.DefaultViews...)
	views = append(views, rpcmetrics.DefaultViews...)
	return views
}()

var ChainNodeViews = append([]*view.View{
	ChainNodeHeightView,
	ChainNodeHeightExpectedView,
	ChainNodeWorkerHeightView,
	BlockReceivedView,
	BlockValidationFailureView,
	BlockValidationSuccessView,
	BlockValidationDurationView,
	BlockDelayView,
	MessagePublishedView,
	MessageReceivedView,
	MessageValidationFailureView,
	MessageValidationSuccessView,
	PubsubPublishMessageView,
	PubsubDeliverMessageView,
	PubsubRejectMessageView,
	PubsubDuplicateMessageView,
	PubsubRecvRPCView,
	PubsubSendRPCView,
	PubsubDropRPCView,
	VMFlushCopyCountView,
	VMFlushCopyDurationView,
	SplitstoreMissView,
	SplitstoreCompactionTimeSecondsView,
	SplitstoreCompactionHotView,
	SplitstoreCompactionColdView,
	SplitstoreCompactionDeadView,

	MessageRevertView,
	MessageRevertBytesView,
	BlockRevertView,
	BlockRevertBytesView,
	SplitstoreBytesView,
	SplitstoreColdBytesView,

	MpoolPendingCountView,

	MessageReceivedBytesView,
	BlockReceivedBytesView,
	ServeSyncSuccessView,
	ServeSyncFailureView,
	ServeSyncBytesView,
	TipsetMessagesCountView,
	TipsetMessagesRateView,
	TipsetPublishDealsCountView,
	TipsetSubmitPoStsCountView,
	TipsetGasUsedView,
	TipsetGasRewardView,
	TipsetGasPenaltyView,
	TipsetGasBurnView,
}, DefaultViews...)

var MinerNodeViews = append([]*view.View{
	WorkerCallsStartedView,
	WorkerCallsReturnedCountView,
	WorkerUntrackedCallsReturnedView,
	WorkerCallsReturnedDurationView,

	CoinbaseBalanceView,
	MinerPowerView,
	MinerSectorView,
	ServeTransferBytesView,
	ServeTransferAcceptView,
	ServeTransferResultView,
}, DefaultViews...)

// SinceInMilliseconds returns the duration of time since the provide time as a float64.
func SinceInMilliseconds(startTime time.Time) float64 {
	return float64(time.Since(startTime).Nanoseconds()) / 1e6
}

func SinceInSeconds(startTime time.Time) float64 {
	return float64(time.Since(startTime).Nanoseconds()) / 1e9
}

// Timer is a function stopwatch, calling it starts the timer,
// calling the returned function will record the duration.
func Timer(ctx context.Context, m *stats.Float64Measure) func() {
	start := time.Now()
	return func() {
		stats.Record(ctx, m.M(SinceInMilliseconds(start)))
	}
}
