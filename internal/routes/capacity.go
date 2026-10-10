// Spark Stream additions. AGPL-3.0, see LICENSE and NOTICE.
package routes

import (
	"bufio"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"

	"EverythingSuckz/fsb/config"

	"github.com/gin-gonic/gin"
)

// LoadCapacity registers GET /capacity. It returns only counters, so it is
// cheap and safe to expose. A router or website backend can poll it to pick
// the least loaded service.
func (e *allRoutes) LoadCapacity(r *Route) {
	r.Engine.GET("/capacity", capacityRoute)
}

// rssMB reads resident memory from /proc (Linux). It returns 0 elsewhere.
func rssMB() float64 {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.ParseFloat(fields[1], 64)
				return kb / 1024
			}
		}
	}
	return 0
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

func capacityRoute(c *gin.Context) {
	active, max := 0, config.ValueOf.MaxActiveStreams
	if slots != nil {
		active = len(slots)
		max = cap(slots)
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"active_streams": active,
		"max_streams":    max,
		"heap_mb":        round1(float64(ms.HeapAlloc) / (1024 * 1024)),
		"rss_mb":         round1(rssMB()),
		"accepting":      active < max,
	})
}
