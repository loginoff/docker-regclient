package api

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type DockerRegistry struct {
	URL    string
	client http.Client
}

type RegistryErrorResponse struct {
	Errors []struct {
		Code    string
		Message string
	}
}

func (re RegistryErrorResponse) Error() string {
	var s string
	for _, err := range re.Errors {
		s = fmt.Sprintf("%s%s - %s\n", s, err.Code, err.Message)
	}
	return s
}

type DockerImage struct {
	Name          string
	Tag           string
	ContentDigest string
	Created       time.Time
}

type Repolist struct {
	Repositories []string
}

type Taglist struct {
	Tags []string
}

//This function will parse the http.Response body
//and send the result using a clojure
type parsefunc func(b *http.Response) error

//This function makes the actual request to the Registry API and does all
//the error handling
func (r *DockerRegistry) do_api_request(req *http.Request, pfunc parsefunc) error {
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 202 {
		decoder := json.NewDecoder(resp.Body)
		var regerr RegistryErrorResponse
		err = decoder.Decode(&regerr)
		if err != nil {
			return errors.New(fmt.Sprintf("ERROR: Unable to parse JSON for HTTP status code %d", resp.StatusCode))
		}
		return regerr
	}

	return pfunc(resp)
}

func (r *DockerRegistry) Repos() ([]string, error) {
	req, err := http.NewRequest("GET", r.URL+"_catalog", nil)
	if err != nil {
		return nil, err
	}
	var rl Repolist
	err = r.do_api_request(req, func(r *http.Response) error {
		decoder := json.NewDecoder(r.Body)
		return decoder.Decode(&rl)
	})
	if err == nil {
		return rl.Repositories, err
	}
	return nil, err
}

func (r *DockerRegistry) Tags(repo string) ([]string, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s%s/tags/list", r.URL, repo), nil)
	if err != nil {
		return nil, err
	}

	var tags Taglist
	err = r.do_api_request(req, func(r *http.Response) error {
		decoder := json.NewDecoder(r.Body)
		return decoder.Decode(&tags)
	})
	if err == nil {
		return tags.Tags, nil
	}
	return nil, err
}

func (r *DockerRegistry) ImageDetails(image string) (*DockerImage, error) {
	//Separate the input image string to repository and tag
	var repo, tag string
	parts := strings.Split(image, ":")
	if len(parts) == 2 {
		repo = parts[0]
		tag = parts[1]
	} else if len(parts) == 1 {
		repo = parts[0]
		tag = "latest"
	} else {
		return nil, errors.New("Image must be in the form 'repository:tag'")
	}

	//We do the first request to the /v2/<repository>/manifests/<tag> endpoint in order
	//to obtain v1Compatibility entries for each image layer. From those we can infer
	//the creation timestamp of the image
	req, err := http.NewRequest("GET", fmt.Sprintf("%s%s/manifests/%s", r.URL, repo, tag), nil)
	if err != nil {
		return nil, err
	}

	var manifest DockerImage

	err = r.do_api_request(req, func(r *http.Response) error {
		var jsoncontent interface{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&jsoncontent)
		if err != nil {
			return err
		}
		toplevel := jsoncontent.(map[string]interface{})
		manifest.Name = toplevel["name"].(string)
		manifest.Tag = toplevel["tag"].(string)

		history := toplevel["history"].([]interface{})[0].(map[string]interface{})["v1Compatibility"].(string)
		json.Unmarshal([]byte(history), &jsoncontent)
		firstlayer := jsoncontent.(map[string]interface{})
		timestring := firstlayer["created"].(string)
		manifest.Created, err = time.Parse("2006-01-02T15:04:05Z", timestring)

		return err
	})

	if err != nil {
		return nil, err
	}

	//We do a second request to the /v2/<repository>/manifests/<tag> endpoint and set a
	//special header in order to get the "correct" Content-Digest, which we can use for deleting
	//the image https://github.com/docker/distribution/issues/1755
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	err = r.do_api_request(req, func(r *http.Response) error {
		manifest.ContentDigest = r.Header["Docker-Content-Digest"][0]
		return nil
	})

	return &manifest, err
}

func (r *DockerRegistry) DeleteImage(img *DockerImage) error {
	req, err := http.NewRequest("DELETE", fmt.Sprintf("%s%s/manifests/%s", r.URL, img.Name, img.ContentDigest), nil)
	if err != nil {
		return err
	}
	return r.do_api_request(req, func(r *http.Response) error {
		return nil
	})
}

func NewDockerRegistry(url string, verify_ssl bool) (*DockerRegistry, error) {
	if strings.HasSuffix(url, "/") {
		url = fmt.Sprintf("%sv2/", url)
	} else {
		url = fmt.Sprintf("%s/v2/", url)
	}

	transport := http.DefaultTransport
	if !verify_ssl {
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	r := DockerRegistry{
		URL: url,
		client: http.Client{
			Timeout:   time.Second * 5,
			Transport: transport,
		},
	}

	resp, err := r.client.Get(url)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return nil, err
	}

	log.Printf("SUCCESS: established connection to %v", url)
	return &r, nil
}
