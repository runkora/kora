package mount

// ServerConfig holds the kernel NFS server settings.
type ServerConfig struct {
	// KubeletDir is where the node's kubelet directory is mounted in the agent's container.
	// Export paths are resolved beneath it.
	KubeletDir string
	// ExportRoot is the tmpfs that holds one bind mount per export. It is the NFSv4 root.
	ExportRoot string
	// ExportsFile is the exports table exportfs reads.
	ExportsFile string
	// Threads is the number of nfsd threads to start.
	Threads int
	// GraceSecs sets the NFSv4 grace and lease times before nfsd starts.
	GraceSecs int
}

// Client mounts sandbox folders with the kernel NFS client.
type Client struct {
	// KubeletDir is where the node's kubelet directory is mounted in the agent's container.
	KubeletDir string
	// Options are the NFS mount options. The server address is added per mount.
	Options string
}
