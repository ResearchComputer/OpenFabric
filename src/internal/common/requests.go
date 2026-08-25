package common

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// RemoteGET performs a GET request with a short timeout and returns the body
// or an error if the request fails or returns a non-2xx status code.
func RemoteGET(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		Logger.Debug("http client: could not create request: ", err)
		return nil, err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		Logger.Debug("http client: request failed: ", err)
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		err = fmt.Errorf("unexpected status code: %d", res.StatusCode)
		Logger.Debug("http client: ", err)
		return nil, err
	}

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		Logger.Debug("http client: could not read response: ", err)
		return nil, err
	}
	return resBody, nil
}
