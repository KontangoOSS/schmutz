package store

import (
	"context"
	"encoding/json"
	"time"
)

type Fingerprint struct {
	ID           string          `json:"id"`
	MachineID    string          `json:"machine_id"`
	Hostname     string          `json:"hostname"`
	SourceIP     string          `json:"source_ip"`
	HardwareHash string          `json:"hardware_hash"`
	OS           string          `json:"os,omitempty"`
	OSVersion    string          `json:"os_version,omitempty"`
	Arch         string          `json:"arch,omitempty"`
	Kernel       string          `json:"kernel,omitempty"`
	CPUModel     string          `json:"cpu_model,omitempty"`
	CPUCores     int             `json:"cpu_cores,omitempty"`
	MemoryMB     int             `json:"memory_mb,omitempty"`
	SerialNumber string          `json:"serial_number,omitempty"`
	MACAddrs     []string        `json:"mac_addrs,omitempty"`
	DiskSerials  []string        `json:"disk_serials,omitempty"`
	SSHHostKeys  []string        `json:"ssh_host_keys,omitempty"`
	SystemHash   string          `json:"system_hash,omitempty"`
	NetworkHash  string          `json:"network_hash,omitempty"`
	FullHash     string          `json:"full_hash,omitempty"`
	BootID       string          `json:"boot_id,omitempty"`
	Timezone     string          `json:"timezone,omitempty"`
	Locale       string          `json:"locale,omitempty"`
	Gateway      string          `json:"gateway,omitempty"`
	DNSServers   []string        `json:"dns_servers,omitempty"`
	OpenPorts    []int           `json:"open_ports,omitempty"`
	PackageCount int             `json:"package_count,omitempty"`
	UptimeSecs   int             `json:"uptime_secs,omitempty"`
	JA4          string          `json:"ja4,omitempty"`
	UserAgent    string          `json:"user_agent,omitempty"`
	Extra        json.RawMessage `json:"extra,omitempty"`
	FirstSeen    time.Time       `json:"first_seen"`
	LastSeen     time.Time       `json:"last_seen"`
	SeenCount    int             `json:"seen_count"`
}

func (s *Store) UpsertFingerprint(ctx context.Context, f Fingerprint) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO machine_fingerprints
		    (machine_id, hostname, source_ip, hardware_hash,
		     os, os_version, arch, kernel, cpu_model, cpu_cores, memory_mb,
		     serial_number, mac_addrs, disk_serials, ssh_host_keys,
		     system_hash, network_hash, full_hash, boot_id, timezone, locale,
		     gateway, dns_servers, open_ports, package_count, uptime_secs,
		     ja4, user_agent, extra)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,
		        $16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29)
		ON CONFLICT (machine_id) DO UPDATE SET
		    hostname      = EXCLUDED.hostname,
		    source_ip     = EXCLUDED.source_ip,
		    hardware_hash = EXCLUDED.hardware_hash,
		    os            = EXCLUDED.os,
		    os_version    = EXCLUDED.os_version,
		    arch          = EXCLUDED.arch,
		    kernel        = EXCLUDED.kernel,
		    cpu_model     = EXCLUDED.cpu_model,
		    cpu_cores     = EXCLUDED.cpu_cores,
		    memory_mb     = EXCLUDED.memory_mb,
		    serial_number = EXCLUDED.serial_number,
		    mac_addrs     = EXCLUDED.mac_addrs,
		    disk_serials  = EXCLUDED.disk_serials,
		    ssh_host_keys = EXCLUDED.ssh_host_keys,
		    system_hash   = EXCLUDED.system_hash,
		    network_hash  = EXCLUDED.network_hash,
		    full_hash     = EXCLUDED.full_hash,
		    boot_id       = EXCLUDED.boot_id,
		    timezone      = EXCLUDED.timezone,
		    locale        = EXCLUDED.locale,
		    gateway       = EXCLUDED.gateway,
		    dns_servers   = EXCLUDED.dns_servers,
		    open_ports    = EXCLUDED.open_ports,
		    package_count = EXCLUDED.package_count,
		    uptime_secs   = EXCLUDED.uptime_secs,
		    ja4           = EXCLUDED.ja4,
		    user_agent    = EXCLUDED.user_agent,
		    extra         = EXCLUDED.extra,
		    last_seen     = now(),
		    seen_count    = machine_fingerprints.seen_count + 1`,
		f.MachineID, f.Hostname, f.SourceIP, f.HardwareHash,
		nilIfEmpty(f.OS), nilIfEmpty(f.OSVersion), nilIfEmpty(f.Arch),
		nilIfEmpty(f.Kernel), nilIfEmpty(f.CPUModel), zeroIfEmpty(f.CPUCores),
		zeroIfEmpty(f.MemoryMB), nilIfEmpty(f.SerialNumber),
		f.MACAddrs, f.DiskSerials, f.SSHHostKeys,
		nilIfEmpty(f.SystemHash), nilIfEmpty(f.NetworkHash), nilIfEmpty(f.FullHash),
		nilIfEmpty(f.BootID), nilIfEmpty(f.Timezone), nilIfEmpty(f.Locale),
		nilIfEmpty(f.Gateway), f.DNSServers, f.OpenPorts,
		zeroIfEmpty(f.PackageCount), zeroIfEmpty(f.UptimeSecs),
		nilIfEmpty(f.JA4), nilIfEmpty(f.UserAgent), f.Extra,
	)
	return err
}

func (s *Store) GetFingerprint(ctx context.Context, machineID string) (*Fingerprint, error) {
	f := &Fingerprint{}
	err := s.pool.QueryRow(ctx, `
		SELECT machine_id, hostname, source_ip, hardware_hash,
		       COALESCE(os,''), COALESCE(os_version,''), COALESCE(arch,''),
		       COALESCE(system_hash,''), COALESCE(network_hash,''), COALESCE(full_hash,''),
		       seen_count, first_seen, last_seen
		FROM machine_fingerprints WHERE machine_id=$1`, machineID,
	).Scan(
		&f.MachineID, &f.Hostname, &f.SourceIP, &f.HardwareHash,
		&f.OS, &f.OSVersion, &f.Arch,
		&f.SystemHash, &f.NetworkHash, &f.FullHash,
		&f.SeenCount, &f.FirstSeen, &f.LastSeen,
	)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *Store) GetFingerprintByHardwareHash(ctx context.Context, hash string) (*Fingerprint, error) {
	f := &Fingerprint{}
	err := s.pool.QueryRow(ctx, `
		SELECT machine_id, hostname, source_ip, hardware_hash,
		       COALESCE(os,''), COALESCE(os_version,''), COALESCE(arch,''),
		       COALESCE(system_hash,''), COALESCE(network_hash,''), COALESCE(full_hash,''),
		       seen_count, first_seen, last_seen
		FROM machine_fingerprints WHERE hardware_hash=$1 LIMIT 1`, hash,
	).Scan(
		&f.MachineID, &f.Hostname, &f.SourceIP, &f.HardwareHash,
		&f.OS, &f.OSVersion, &f.Arch,
		&f.SystemHash, &f.NetworkHash, &f.FullHash,
		&f.SeenCount, &f.FirstSeen, &f.LastSeen,
	)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *Store) GetFingerprintBySourceIP(ctx context.Context, ip string) (*Fingerprint, error) {
	f := &Fingerprint{}
	err := s.pool.QueryRow(ctx, `
		SELECT machine_id, hostname, source_ip, hardware_hash, seen_count, first_seen, last_seen
		FROM machine_fingerprints WHERE source_ip=$1 LIMIT 1`, ip,
	).Scan(&f.MachineID, &f.Hostname, &f.SourceIP, &f.HardwareHash, &f.SeenCount, &f.FirstSeen, &f.LastSeen)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func zeroIfEmpty(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
