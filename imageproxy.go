package supersizeme

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"appengine"
	"appengine/urlfetch"

	"github.com/mjibson/goon"
)

func init() {
	http.HandleFunc("/", HandleImageRequest)
}

type optionsT struct {
	Url    *url.URL
	Width  int64
	Height int64
}

type Image struct {
	Id        string `datastore:"-" goon:"id"`
	Url       string
	Width     int64
	Height    int64
	Image     []byte
	FetchTime time.Time
}

func HandleImageRequest(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)

	i, err := NewImage(r)
	if err != nil {
		c.Errorf("Failed: %s", err)
		http.Error(w, fmt.Sprintf("%s", err), 400)
		return
	}

	g := goon.FromContext(c)
	err = g.Get(i)
	if err != nil {
		c.Warningf("Loading from source (%s, %d, %d): %s", i.Url, i.Width, i.Height, err)
		// maybe to do this in a channel/worker, so the number of concurrent requests for same image will be = # of instances?
		err = loadImage(c, i)
		// TODO:
		// 1. in case image loaded, but saving failed, serve image itself
		// 2. in case failed loading image, incr some counter and if counter > THRESHOLD return 404
		// 3. in case failed loading image and counter < THRESHOLD, redirect to image
		if err != nil {
			http.Error(w, fmt.Sprintf("%s", err), 500)
			return
		}
	}

	// TODO: cache headers
	w.Header().Add("Content-Type", "image/jpeg")
	w.Write(i.Image)
}

var urlFix = regexp.MustCompile("^(https?:/)([^/])")

func NewImage(r *http.Request) (*Image, error) {
	parts := strings.SplitN(r.URL.Path, "/", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("Wrong arguments (%s -> %s)", r.URL.Path, parts)
	}

	size := strings.SplitN(parts[1], "x", 2)
	h, err := strconv.ParseInt(size[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("Failed parsing size (%s -> %s -> %s)", r.URL.Path, parts, size)
	}

	w, err := strconv.ParseInt(size[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("Failed parsing size (%s)", size)
	}

	urlRaw, err := url.QueryUnescape(parts[2])
	if err != nil {
		urlRaw = parts[2]
	}

	// GAE messes the url and transforms http:// into http:/ :
	urlRaw = urlFix.ReplaceAllString(urlRaw, "$1/$2")

	url, err := url.Parse(urlRaw)
	if err != nil {
		return nil, fmt.Errorf("Failed parsing url (%s): %s", parts[2], err)
	}

	if !url.IsAbs() {
		url.Scheme = "http"
	}

	i := Image{
		Id:     r.URL.Path,
		Url:    url.String(),
		Width:  w,
		Height: h,
	}

	return &i, nil
}

func loadImage(c appengine.Context, i *Image) error {
	i.FetchTime = time.Now()
	client := urlfetch.Client(c)
	resp, err := client.Get(i.Url)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	reader, _ := resp.Body.(io.Reader)

	img, err := CenterCrop(&reader, int(i.Width), int(i.Height))
	if err != nil {
		return err
	}

	i.Image = img

	g := goon.FromContext(c)
	_, err = g.Put(i)
	if err != nil {
		c.Errorf("Failed storing goon: %s", err)
	}

	return nil
}
