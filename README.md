# M3U8 proxy

A simple proxy server for M3U8 file written in go for high performance

### Running the server

copy the `.env.example` file to `.env`  
it takes two env  
PORT and CORS_DOMAIN  
add multiple domain separated by comma (,)

`go run cmd/main.go`

or using docker

`docker run --rm -d -p 4040:4040 -e PORT=4040 -e CORS_DOMAIN=localhost:3000 dovakiin0/proxy-m3u8:latest`

or build yourself using Dockerfile

### Usage

Request the proxy server on `/m3u8-proxy?url=<original_m3u8_url>&referer=<referer_url>`. referer is optional
