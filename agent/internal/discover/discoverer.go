package discover

import (
	"log"
	"time"
)

type Discoverer struct {
	schmutzDir string
	baoAddr    string
	baoToken   string
	slug       string
	interval   time.Duration
	ports      []uint16
	stop       chan struct{}
}

func NewDiscoverer(schmutzDir, baoAddr, baoToken string) *Discoverer {
	return &Discoverer{
		schmutzDir: schmutzDir,
		baoAddr:    baoAddr,
		baoToken:   baoToken,
		interval:   10 * time.Minute,
		ports:      CommonPorts,
		stop:       make(chan struct{}),
	}
}

func (d *Discoverer) WithSlug(slug string) *Discoverer {
	d.slug = slug
	return d
}

func (d *Discoverer) WithInterval(interval time.Duration) *Discoverer {
	d.interval = interval
	return d
}

func (d *Discoverer) Run() {
	d.runOnce()
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-d.stop:
			return
		case <-ticker.C:
			d.runOnce()
		}
	}
}

func (d *Discoverer) Stop() {
	select {
	case <-d.stop:
	default:
		close(d.stop)
	}
}

func (d *Discoverer) runOnce() {
	log.Printf("discover: scanning %d ports on localhost", len(d.ports))
	targets, err := ScanLocalhost(d.ports)
	if err != nil {
		log.Printf("discover: scan error: %v", err)
		return
	}
	log.Printf("discover: found %d open services", len(targets))
	slug := d.slug
	if slug == "" {
		slug = "unknown"
	}
	result := BuildResult(targets, slug)
	pub := NewPublisher(d.schmutzDir, d.baoAddr, d.baoToken)
	if err := pub.Publish(result); err != nil {
		log.Printf("discover: publish error: %v", err)
		return
	}
	log.Printf("discover: schema published (%d services)", len(result.Services))
}
