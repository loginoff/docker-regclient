package main

import (
	"bufio"
	"docker-registry/api"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli"
)

func handleErr(e error) {
	if e != nil {
		log.Printf("ERROR: %v\n", e)
	}
}

type ImgFilter func(img *api.DockerImage) bool

type ByCreated []*api.DockerImage

func (s ByCreated) Len() int           { return len(s) }
func (s ByCreated) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s ByCreated) Less(i, j int) bool { return s[i].Created.After(s[j].Created) }

func init_registry(c *cli.Context) *api.DockerRegistry {
	r, err := api.NewDockerRegistry(c.GlobalString("url"), c.GlobalBool("verify-tls"))
	if err != nil {
		log.Fatalf("Unable to connect to Docker registry at %s: %v", c.String("url"), err)
	}
	return r
}

//This function allows us to concurrently fetch images for all tags contained
//in the specified repos
func fetch_images(r *api.DockerRegistry, repos []string, filters []ImgFilter) []*api.DockerImage {
	//Let's allow only 10 requests per second
	rate := time.Second / 10
	throttle := time.Tick(rate)

	type repotags struct {
		repo string
		tags []string
	}
	tagschan := make(chan *repotags)
	imgchan := make(chan *api.DockerImage)

	var tagwait sync.WaitGroup
	var imgwait sync.WaitGroup

	for _, currepo := range repos {
		tagwait.Add(1)
		currepo := currepo
		<-throttle
		go func() {
			curtags, err := r.Tags(currepo)
			if err == nil {
				tagschan <- &repotags{currepo, curtags}
			}
			tagwait.Done()
		}()
	}
	go func() { tagwait.Wait(); close(tagschan) }()

	for currepotags := range tagschan {
		for _, tag := range currepotags.tags {
			imgwait.Add(1)
			//This is necessary to use "tag" from inside the clojure
			tag := tag
			<-throttle
			go func() {
				img, err := r.ImageDetails(currepotags.repo + ":" + tag)
				if err == nil {
					imgchan <- img
				} else {
					log.Printf("Unable to get image (%s:%s): %s", currepotags.repo, tag, err)
				}
				imgwait.Done()
			}()
		}
	}
	go func() { imgwait.Wait(); close(imgchan) }()

	//Collect all the result images and sort by creation date
	var imgs []*api.DockerImage
Outer:
	for img := range imgchan {
		for _, filter := range filters {
			if !filter(img) {
				continue Outer
			}
		}
		imgs = append(imgs, img)
	}

	sort.Sort(ByCreated(imgs))
	return imgs
}

func main() {
	app := cli.NewApp()
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "url, u",
			Value: "http://localhost",
			Usage: "The URL of your Docker Registry",
		},
		cli.BoolFlag{
			Name:  "verify-tls, k",
			Usage: "Verify the TLS cetificate of the registry",
		},
	}

	app.Action = func(c *cli.Context) error {
		init_registry(c)
		return nil
	}

	app.Commands = []cli.Command{
		{
			Name:  "repos",
			Usage: "Display a list of repositories in the registry",
			Action: func(c *cli.Context) error {
				r := init_registry(c)
				repos, err := r.Repos()
				if err != nil {
					return cli.NewExitError(err.Error(), 1)
				}
				for _, repo := range repos {
					tags, _ := r.Tags(repo)
					fmt.Printf("%s (%d tags)\n", repo, len(tags))
				}
				return nil
			},
		},
		{
			Name:  "images",
			Usage: "Display images from specified repositories",
			Flags: []cli.Flag{
				cli.StringSliceFlag{
					Name: "repo, r",
				},
				cli.StringFlag{
					Name: "older-than",
				},
				cli.StringFlag{
					Name: "tag-contains",
				},
				cli.StringFlag{
					Name: "tag-exclude",
				},
				cli.BoolFlag{
					Name: "delete",
				},
			},
			Action: func(c *cli.Context) error {
				repos := c.StringSlice("repo")
				if len(repos) == 0 {
					return cli.NewExitError("You must specify at least one repository", 1)
				}

				filters := make([]ImgFilter, 0)

				if older := c.String("older-than"); older != "" {
					t, err := time.Parse("2006-01-02", older)
					if err != nil {
						return cli.NewExitError(err.Error(), 1)
					}
					filters = append(filters, func(img *api.DockerImage) bool {
						return img.Created.Before(t)
					})
				}

				if contains := c.String("tag-contains"); contains != "" {
					filters = append(filters, func(img *api.DockerImage) bool {
						return strings.Contains(img.Tag, contains)
					})
				}

				if exclude := c.String("tag-exclude"); exclude != "" {
					filters = append(filters, func(img *api.DockerImage) bool {
						return !strings.Contains(img.Tag, exclude)
					})
				}

				r := init_registry(c)
				imgs := fetch_images(r, repos, filters)
				if len(imgs) == 0 {
					return nil
				}

				for _, img := range imgs {
					fmt.Printf("%s %s %s:%s\n", img.Created.Format("2006-01-02 15:04:05"), img.ContentDigest[:16], img.Name, img.Tag)
				}
				if c.Bool("delete") {
				Inputloop:
					for {
						reader := bufio.NewReader(os.Stdin)
						fmt.Printf("Do you really want to delete these %d images? (y/n): ", len(imgs))
						ans, _ := reader.ReadString('\n')
						switch strings.TrimSpace(ans) {
						case "y":
							break Inputloop
						case "n":
							return nil
						}
					}
					for _, img := range imgs {
						fmt.Printf("Deleting (%s:%s): ", img.Name, img.Tag)
						err := r.DeleteImage(img)
						if err == nil {
							fmt.Printf("SUCCESS\n")
						} else {
							fmt.Println(err)
						}
					}
				}
				return nil
			},
		},
		{
			Name:  "delete",
			Usage: "Reads lines containing repository:tag from STDIN and deletes the respective images from the Registry",
			Action: func(c *cli.Context) error {
				r := init_registry(c)
				imgs := fetch_images(r, []string{"rush/portal"}, nil)
				portalimg := imgs[len(imgs)-1]
				log.Println(portalimg)

				err := r.DeleteImage(portalimg)
				if err != nil {
					log.Printf("ERROR deleting image (%s:%s): %v", portalimg.Name, portalimg.Tag, err)
				}
				return nil
			},
		},
	}
	app.Run(os.Args)
}
