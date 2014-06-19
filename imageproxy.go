package supersizeme

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	// "time"

	"appengine"
	"appengine/blobstore"
	"appengine/image"
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
	Id     string `datastore:"-" goon:"id"`
	Url    string
	Width  int64
	Height int64

	BlobKey    appengine.BlobKey
	ServingUrl string
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

	reader := blobstore.NewReader(c, i.BlobKey)
	w.Header().Add("Content-Type", "image/jpeg")
	// time should come from i
	// http.ServeContent(w, r, "", time.Now(), reader)
	// http.Redirect(w, r, i.ServingUrl, 301)
	io.Copy(w, reader)
}

func loadImage(c appengine.Context, i *Image) error {
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

	err = storeAndSetServingUrl(c, i, img)

	return err
}

func storeAndSetServingUrl(c appengine.Context, i *Image, img []byte) error {
	w, err := blobstore.Create(c, "image/jpeg")
	if err != nil {
		return err
	}

	_, err = w.Write(img)
	if err != nil {
		return err
	}

	err = w.Close()
	if err != nil {
		return err
	}

	k, err := w.Key()
	if err != nil {
		return err
	}

	servingUrl, err := image.ServingURL(c, k, nil)
	if err != nil {
		return err
	}

	i.ServingUrl = servingUrl.String()
	i.BlobKey = k

	g := goon.FromContext(c)
	_, err = g.Put(i)
	if err != nil {
		c.Errorf("Failed storing goon: %s", err)
	}

	return nil
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
