package handler

import (
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/dovakiin0/proxy-m3u8/internal/utils"
	"github.com/labstack/echo/v4"
)

func isStaticFileExtension(path string) bool {
	lowerPath := strings.ToLower(path)
	for _, ext := range utils.AllowedExtensions {
		if strings.HasSuffix(lowerPath, ext) {
			return true
		}
	}
	return false
}

func M3U8Proxy(c echo.Context) error {
	targetURL := c.QueryParam("url")
	if targetURL == "" {
		return c.String(http.StatusBadRequest, "Missing 'url' query parameter")
	}

	referer := c.QueryParam("referer")
	refererHeader, err := url.QueryUnescape(referer)
	if err != nil {
		log.Printf("Error unescaping referer: %v", err)
		return c.String(http.StatusBadRequest, "Invalid 'referer' query parameter")
	}

	_, err = url.ParseRequestURI(targetURL)
	if err != nil {
		log.Printf("Invalid target URL: %s, error: %v", targetURL, err)
		return c.String(http.StatusBadRequest, "Invalid 'url' query parameter")
	}
	isM3U8 := strings.HasSuffix(strings.ToLower(targetURL), ".m3u8")
	isTS := strings.HasSuffix(strings.ToLower(targetURL), ".ts")
	isOtherStatic := isStaticFileExtension(targetURL)

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		log.Printf("Error creating request to target %s: %v", targetURL, err)
		return c.String(http.StatusInternalServerError, "Failed to create request to target server")
	}

	req.Header.Set("Accept", "*/*")
	// if the referer is provided, set it in the request headers
	if refererHeader != "" {
		req.Header.Set("Referer", refererHeader)
		req.Header.Set("Origin", refererHeader)
	} else {
		// use the default referer if not provided, for gogo and hianime, this is normally provided
		req.Header.Set("Origin", "https://megacloud.blog/")
		req.Header.Set("Referer", "https://megacloud.blog/")
	}

	upstreamResp, err := utils.ProxyHTTPClient.Do(req)
	if err != nil {
		log.Printf("Error fetching target URL %s: %v", targetURL, err)
		// Check for timeout or other specific errors if needed
		if urlErr, ok := err.(*url.Error); ok && urlErr.Timeout() {
			return c.String(http.StatusGatewayTimeout, "Upstream server timed out")
		}
		return c.String(http.StatusBadGateway, "Failed to fetch content from upstream server")
	}
	defer upstreamResp.Body.Close()

	// Whitelist headers to copy
	headerWhitelist := []string{
		"Content-Type", "Content-Disposition", "Accept-Ranges", "Content-Range",
	}
	if upstreamResp.StatusCode == http.StatusOK || upstreamResp.StatusCode == http.StatusPartialContent {
		headerWhitelist = append(headerWhitelist, "ETag", "Last-Modified")
	}

	for _, hName := range headerWhitelist {
		if hVal := upstreamResp.Header.Get(hName); hVal != "" {
			c.Response().Header().Set(hName, hVal)
		}
	}

	// Special handling for Content-Length based on your Node.js logic
	// If we are transforming the content (M3U8/TS), the length will change, so remove it.
	// For direct pipe (isOtherStatic), Content-Length from upstream is fine.
	if isM3U8 || isTS {
		// Content will be transformed, so original Content-Length is invalid.
		// Streaming responses with HTTP/1.1 will use chunked transfer encoding if Content-Length is not set.
		// For HTTP/2, length is less critical.
		c.Response().Header().Del("Content-Length")
	} else if contentLength := upstreamResp.Header.Get("Content-Length"); contentLength != "" && isOtherStatic {
		c.Response().Header().Set("Content-Length", contentLength)
	}

	c.Response().WriteHeader(upstreamResp.StatusCode)

	// If it's a "static file" (not m3u8 or ts that needs line transformation)
	// or if the upstream response indicates an error, just pipe it.
	if upstreamResp.StatusCode != http.StatusOK {
		log.Printf("Upstream server for %s returned status %d", targetURL, upstreamResp.StatusCode)
		_, err = io.Copy(c.Response().Writer, upstreamResp.Body)
		if err != nil {
			log.Printf("Error piping non-OK response for %s: %v", targetURL, err)
			// c.Response() has already been written to, so we can't easily send a different error here.
			// The error will be in the server logs.
		}
		return nil // Status and headers already set
	}

	if isOtherStatic { // .png, .jpg etc. (already checked for status OK)
		_, err = io.Copy(c.Response().Writer, upstreamResp.Body)
		if err != nil {
			log.Printf("Error piping static file %s: %v", targetURL, err)
			// Connection might be closed by client
		}
		return nil
	}

	proxyRoutePath := c.Path()
	if strings.HasPrefix(proxyRoutePath, "/") {
		proxyRoutePath = strings.TrimPrefix(proxyRoutePath, "/")
	}
	urlPrefix := proxyRoutePath + "?url="

	err = utils.ProcessM3U8Stream(upstreamResp.Body, c.Response().Writer, targetURL, urlPrefix)
	if err != nil {
		log.Printf("Error processing M3U8 stream for %s: %v", targetURL, err)
		// Don't try to write another error response if headers already sent
		// The error will be in the server logs.
		return nil
	}

	return nil
}
