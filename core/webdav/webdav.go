package webdav

import (
	"errors"
	"fmt"
	"github.com/juju/ratelimit"
	"golang.org/x/net/webdav"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Strategy struct {
	SubDir    string
	UpRate    int64
	DownRate  int64
	MaxVolume int64
}

// WebDav service manager
type Server struct {
	root     string
	handlers sync.Map
}

func NewServer(root string) *Server {
	server := Server{root: root}
	return &server
}

func (s *Server) ServeWebDav(w http.ResponseWriter, r *http.Request, t Strategy) {
	handler, ok := s.loadHandler(t.SubDir)
	if !ok {
		http.Error(w, "WebDAV: Can not find user handler!", http.StatusInternalServerError)
		return
	}

	if r.Method == "GET" && t.DownRate > 0 {
		s.serveGet(w, r, handler, float64(t.DownRate))
	} else if r.Method == "PUT" && t.UpRate > 0 {
		s.servePut(w, r, handler, float64(t.UpRate))
	} else {
		s.serveHTTP(w, r, handler)
	}
}

func (s *Server) loadHandler(dir string) (*webdav.Handler, bool) {
	var handler *webdav.Handler
	v, ok := s.handlers.Load(dir)
	if !ok {
		handler = &webdav.Handler{
			FileSystem: webdav.Dir(filepath.Join(s.root, dir)),
			LockSystem: webdav.NewMemLS(),
		}

		if v, ok := s.handlers.LoadOrStore(dir, handler); ok {
			handler, ok = v.(*webdav.Handler)
			if !ok {
				return nil, false
			}
		}
	} else {
		handler, ok = v.(*webdav.Handler)
		if !ok {
			return nil, false
		}
	}

	return handler, handler != nil
}

func (s *Server) serveGet(w http.ResponseWriter, r *http.Request, h *webdav.Handler, rate float64) {
	reqPath := r.URL.Path
	f, err := h.FileSystem.OpenFile(r.Context(), reqPath, os.O_RDONLY, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if fi.IsDir() {
		http.Error(w, "can not get a folder directly!", http.StatusMethodNotAllowed)
		return
	}

	bucket := ratelimit.NewBucketWithRate(rate, 4*1024*1024)
	size, err := s.parseSize(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	rangeArray, err := s.parseHTTPRange(r, size)
	if err != nil {
		http.Error(w, err.Error(), http.StatusRequestedRangeNotSatisfiable)
		return
	}

	var httpCode = http.StatusOK
	var sendSize = size
	var sendContent io.Reader = f

	if len(rangeArray) == 1 {
		ra := rangeArray[0]
		if _, err := f.Seek(ra.start, io.SeekStart); err != nil {
			http.Error(w, err.Error(), http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", ra.contentRange(size))

		httpCode = http.StatusPartialContent
		sendSize = ra.length
	} else if len(rangeArray) > 1 {
		pr, pw := io.Pipe()
		mw := multipart.NewWriter(pw)
		w.Header().Set("Content-Type", "multipart/byteranges; boundary="+mw.Boundary())

		httpCode = http.StatusPartialContent
		sendSize = s.parseTotalRange(rangeArray, mime.TypeByExtension(path.Ext(reqPath)), size)
		sendContent = pr
		defer pr.Close()

		go func() {
			for _, ra := range rangeArray {
				part, err := mw.CreatePart(ra.mimeHeader(mime.TypeByExtension(path.Ext(reqPath)), size))
				if err != nil {
					pw.CloseWithError(err)
					return
				}
				if _, err := f.Seek(ra.start, io.SeekStart); err != nil {
					pw.CloseWithError(err)
					return
				}
				if _, err := io.CopyN(part, f, ra.length); err != nil {
					pw.CloseWithError(err)
					return
				}
			}
			mw.Close()
			pw.Close()
		}()
	}

	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(sendSize, 10))
	w.WriteHeader(httpCode)

	_, err = io.CopyN(ratelimit.Writer(w, bucket), sendContent, sendSize)
	if err != nil {
		http.Error(w, err.Error(), http.StatusMethodNotAllowed)
		return
	}
}

func (s *Server) servePut(w http.ResponseWriter, r *http.Request, h *webdav.Handler, rate float64) {
	reqPath := r.URL.Path
	now := time.Now()

	token, err := h.LockSystem.Create(now, webdav.LockDetails{
		Root:      reqPath,
		Duration:  -1,
		ZeroDepth: true,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusPreconditionFailed)
		return
	}
	defer func() {
		if token != "" {
			h.LockSystem.Unlock(now, token)
		}
	}()

	file, err := h.FileSystem.OpenFile(r.Context(), reqPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer file.Close()

	bucket := ratelimit.NewBucketWithRate(rate, 4*1024*1024)
	_, err = io.Copy(file, ratelimit.Reader(r.Body, bucket))
	if err != nil {
		http.Error(w, err.Error(), http.StatusMethodNotAllowed)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request, h *webdav.Handler) {
	h.ServeHTTP(w, r)
}

func (s *Server) parseSize(content io.ReadSeeker) (int64, error) {
	size, err := content.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	_, err = content.Seek(0, io.SeekStart)
	if err != nil {
		return 0, err
	}
	return size, nil
}

func (s *Server) parseHTTPRange(r *http.Request, size int64) ([]httpRange, error) {
	rangeMap := strings.Split(r.Header.Get("Range"), "=")
	if len(rangeMap) != 2 || strings.EqualFold(rangeMap[0], "bytes=") {
		return nil, nil
	}

	var rangeArray []httpRange

	rangeInfos := strings.Split(rangeMap[1], ",")
	for _, rangeInfo := range rangeInfos {
		rangeInfo = strings.TrimSpace(rangeInfo)
		if rangeInfo == "" {
			continue
		}
		i := strings.Index(rangeInfo, "-")
		if i < 0 {
			return nil, errors.New("invalid range")
		}

		rangePos := httpRange{0, 0}
		start, end := strings.TrimSpace(rangeInfo[:i]), strings.TrimSpace(rangeInfo[i+1:])
		if start == "" {
			lastSize, err := strconv.ParseInt(end, 10, 64)
			if err != nil {
				return nil, errors.New("invalid end range")
			}
			if lastSize > size {
				lastSize = size
			}
			rangePos.start = size - lastSize
			rangePos.length = lastSize
		} else {
			startPos, err := strconv.ParseInt(start, 10, 64)
			if err != nil || startPos < 0 {
				return nil, errors.New("invalid start range")
			}
			if startPos >= size {
				return nil, errors.New("invalid start range")
			}
			rangePos.start = startPos

			if end == "" {
				rangePos.length = size - startPos
			} else {
				endPos, err := strconv.ParseInt(end, 10, 64)
				if err != nil || startPos > endPos {
					return nil, errors.New("invalid range")
				}
				if endPos >= size {
					endPos = size - 1
				}
				rangePos.length = endPos - startPos + 1
			}
		}

		rangeArray = append(rangeArray, rangePos)
	}

	return rangeArray, nil
}

func (s *Server) parseTotalRange(ranges []httpRange, contentType string, contentSize int64) (encSize int64) {
	var w countingWriter
	mw := multipart.NewWriter(&w)
	for _, ra := range ranges {
		mw.CreatePart(ra.mimeHeader(contentType, contentSize))
		encSize += ra.length
	}
	mw.Close()
	encSize += int64(w)
	return
}

// httpRange specifies the byte range to be sent to the client.
type httpRange struct {
	start  int64
	length int64
}

func (r httpRange) contentRange(size int64) string {
	return fmt.Sprintf("bytes %d-%d/%d", r.start, r.start+r.length-1, size)
}

func (r httpRange) mimeHeader(contentType string, size int64) textproto.MIMEHeader {
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return textproto.MIMEHeader{
		"Content-Range": {r.contentRange(size)},
		"Content-Type":  {contentType},
	}
}

// countingWriter counts how many bytes have been written to it.
type countingWriter int64

func (w *countingWriter) Write(p []byte) (n int, err error) {
	*w += countingWriter(len(p))
	return len(p), nil
}
