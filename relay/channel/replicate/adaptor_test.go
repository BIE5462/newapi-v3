package replicate

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDoResponsePreservesImageDownloadURLOn404(t *testing.T) {
	oldFetchSetting := *system_setting.GetFetchSetting()
	*system_setting.GetFetchSetting() = system_setting.FetchSetting{
		EnableSSRFProtection: false,
	}
	t.Cleanup(func() { *system_setting.GetFetchSetting() = oldFetchSetting })
	service.InitHttpClient()

	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(imageServer.Close)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	info := &relaycommon.RelayInfo{
		Request: &dto.ImageRequest{ResponseFormat: "b64_json"},
	}
	responseBody := `{"status":"succeeded","output":["` + imageServer.URL + `/generated.png"]}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(responseBody)),
		Header:     make(http.Header),
	}

	_, apiErr := (&Adaptor{}).DoResponse(c, resp, info)

	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeBadResponse, apiErr.GetErrorCode())
	require.Contains(t, apiErr.ToOpenAIError().Message, imageServer.URL+"/generated.png")
	require.Contains(t, apiErr.ToOpenAIError().Message, "HTTP 404")
}
