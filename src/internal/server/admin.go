package server

import (
	"fmt"
	"net/http"
	"opentela/internal/common"
	"opentela/internal/protocol"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// nameRe matches admin-register payload service names. We restrict the
// space to the convbench- prefix so this admin route cannot be used to
// register arbitrary names.
var nameRe = regexp.MustCompile(`^convbench-[A-Za-z0-9_-]{1,80}$`)

type adminRegisterReq struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

type adminRegisterResp struct {
	RegisteredAtNs int64 `json:"registered_at_ns"`
}

func validateAdminRegisterReq(r adminRegisterReq) error {
	if !nameRe.MatchString(r.Name) {
		return fmt.Errorf("name must match ^convbench-[A-Za-z0-9_-]{1,80}$")
	}
	return nil
}

// adminRegisterHandler writes a synthetic Service into the CRDT through
// the existing registrar path. Bound to 127.0.0.1 only via the admin
// listener in StartAdminServer; never mounted on the public router.
func adminRegisterHandler(c *gin.Context) {
	var req adminRegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateAdminRegisterReq(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	protocol.RegisterAdHocService(req.Name, strconv.Itoa(req.Port), nil)
	c.JSON(http.StatusOK, adminRegisterResp{
		RegisteredAtNs: time.Now().UnixNano(),
	})
}

func adminResyncHandler(c *gin.Context) {
	if err := protocol.TriggerResync(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// StartAdminServer launches a separate http.Server exposing /v1/_admin/*
// routes. Used by contrib/benchmark_convergence to inject CRDT writes.
// The bind address defaults to 127.0.0.1 (loopback only) and can be
// overridden via admin.bind (e.g. 0.0.0.0 in Docker integration tests).
func StartAdminServer() {
	adminR := gin.New()
	adminR.POST("/v1/_admin/register", adminRegisterHandler)
	adminR.POST("/v1/_admin/resync", adminResyncHandler)

	addr := viper.GetString("admin.bind") + ":" + viper.GetString("admin.port")
	srv := &http.Server{
		Addr:    addr,
		Handler: adminR,
	}
	common.Logger.Infof("Admin route enabled on %s", addr)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			common.Logger.Errorf("admin server failed: %v", err)
		}
	}()
}
