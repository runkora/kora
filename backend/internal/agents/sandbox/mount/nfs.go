package mount

// ServerConfig holds the kernel NFS server settings.
type ServerConfig struct {
	// KubeletDir is where the node's kubelet directory is mounted in the agent's container.
	// Export paths are resolved beneath it.
	KubeletDir string
	// ExportRoot is the tmpfs that holds one directory per client node, the
	// client's NFSv4 root, with one bind mount per folder exported to it.
	ExportRoot string
	// ExportsFile is the NFS exports table, /etc/exports by default.
	ExportsFile string
	// Threads is the number of nfsd threads to start.
	Threads int
	// GraceSecs sets the NFSv4 grace and lease times before nfsd starts.
	GraceSecs int
	// Port is the TCP port nfsd listens on.
	Port int
	// TrackingDir holds nfsdcld's client records. It must outlive the agent's container.
	TrackingDir string
}

// Client mounts sandbox folders with the kernel NFS client.
type Client struct {
	// KubeletDir is where the node's kubelet directory is mounted in the agent's container.
	KubeletDir string
	// HostKubeletDir is the kubelet directory's path on the node.
	HostKubeletDir string
	// Port is the TCP port of the nfsd it mounts from.
	Port int
	// Options are the NFS mount options. The server address and port are added per mount.
	Options string
}
