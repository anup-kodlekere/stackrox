package env

import "time"

var (
	// NodeRescanInterval sets the time between periodic node rescans for changed components. Results are cached in case they need to be resubmitted for NodeScanCacheDuration.
	NodeRescanInterval = registerDurationSetting("ROX_NODE_RESCAN_INTERVAL", 4*time.Hour)

	// NodeScanCacheDuration defines the time after which a cached inventory is considered outdated. Defaults to 90% of rescan interval.
	NodeScanCacheDuration = registerDurationSetting("ROX_NODE_SCAN_CACHE_TIME", time.Duration(NodeRescanInterval.DurationSetting().Nanoseconds()-NodeRescanInterval.DurationSetting().Nanoseconds()/10))

	// NodeScanInitialBackoff defines the initial time in seconds a Node scan will be delayed if a backoff file is found
	NodeScanInitialBackoff = registerDurationSetting("ROX_NODE_SCAN_INITIAL_BACKOFF", 30*time.Second)

	// NodeScanMaxBackoff is the upper boundary of backoff. Defaults to 5m in seconds, being 50% of Kubernetes restart policy stability timer.
	NodeScanMaxBackoff = registerDurationSetting("ROX_NODE_SCAN_MAX_BACKOFF", 300*time.Second)
)
