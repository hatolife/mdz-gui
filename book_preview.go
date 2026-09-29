package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// attachBookPreview はmdBookの本文を別オリジンで配信し、目次をアプリ側に一本化します。
func attachBookPreview(upstream string) (*http.Server, string, error) {
	target, err := url.Parse(upstream)
	if err != nil {
		return nil, "", err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	original := proxy.Director
	proxy.Director = func(r *http.Request) { original(r); r.Header.Del("Accept-Encoding") }
	proxy.ModifyResponse = func(r *http.Response) error {
		if !strings.Contains(r.Header.Get("Content-Type"), "text/html") {
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, 32<<20+1))
		r.Body.Close()
		if err != nil {
			return err
		}
		if len(data) > 32<<20 {
			return fmt.Errorf("プレビューのHTMLが大きすぎます")
		}
		injection := `<style id="mdz-reader-layout">:root{--sidebar-width:0px!important}.sidebar,.sidebar-resize-handle,#sidebar-toggle,#sidebar-toggle-anchor,#mdbook-sidebar-toggle{display:none!important}.page-wrapper{margin-left:0!important;transform:none!important}.content{max-width:900px}.menu-title{display:none}</style><script>(function(){var applying=false;function ratio(){var d=document.documentElement,r=Math.max(0,d.scrollHeight-innerHeight);return r>0?Math.max(0,Math.min(1,scrollY/r)):0}function notify(){if(!applying)window.parent.postMessage({type:'mdz-book-scroll',ratio:ratio()},'*')}window.addEventListener('DOMContentLoaded',function(){window.parent.postMessage({type:'mdz-book-page',path:window.location.pathname},'*')});window.addEventListener('scroll',notify,{passive:true});window.addEventListener('message',function(e){if(e.source!==window.parent||e.data?.type!=='mdz-book-scroll-to')return;var value=Number(e.data.ratio);if(!Number.isFinite(value))return;applying=true;var range=Math.max(0,document.documentElement.scrollHeight-innerHeight);scrollTo(0,Math.max(0,Math.min(1,value))*range);requestAnimationFrame(function(){applying=false})});window.addEventListener('keydown',function(e){if((e.ctrlKey||e.metaKey)&&e.key.toLowerCase()==='s'){e.preventDefault();window.parent.postMessage({type:'mdz-book-save',saveAs:e.shiftKey},'*')}})})();</script>`
		data = bytes.Replace(data, []byte("</head>"), []byte(injection+"</head>"), 1)
		r.Body = io.NopCloser(bytes.NewReader(data))
		r.ContentLength = int64(len(data))
		r.Header.Set("Content-Length", fmt.Sprint(len(data)))
		r.Header.Del("ETag")
		r.Header.Set("Cache-Control", "no-store")
		return nil
	}
	server := &http.Server{Handler: proxy}
	go func() { _ = server.Serve(listener) }()
	return server, "http://" + listener.Addr().String() + "/", nil
}
