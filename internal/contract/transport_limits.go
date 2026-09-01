package contract

const (
	// WorkerPlaneMaxRequestBytes bounds authenticated Worker Plane request bodies.
	// Job completion is the largest request on that plane.
	WorkerPlaneMaxRequestBytes int64 = 10 << 20

	// MaxApplicationWireResponseBodyBytes leaves enough room for RFC 4648 Base64
	// expansion and the Job completion JSON envelope inside WorkerPlaneMaxRequestBytes.
	MaxApplicationWireResponseBodyBytes int64 = 7 << 20
)
