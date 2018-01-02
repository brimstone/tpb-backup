package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	beegoorm "github.com/astaxie/beego/orm"
	"github.com/gocolly/colly"
	"github.com/gocolly/colly/proxy"
)

type Torrent struct {
	ID         int64     `orm:"pk;column(id)"`
	Title      string    `orm:"type(text)"`
	Category   *Category `orm:"rel(fk)"`
	InfoHash   string    // TODO figure out this exact size
	Size       int64
	Files      int64
	InfoURL    string `orm:"column(info_url)"`
	LangSpoken string
	LangTexted string
	Tags       []*Tag    `orm:"rel(m2m)"`
	Uploaded   time.Time `orm:"type(datetime)"`
	Uploader   *Uploader `orm:"rel(fk)"`
}

type Category struct {
	ID   int64  `orm:"pk;column(id)"`
	Name string `orm:"type(text)"`
}
type Tag struct {
	ID       int64      `orm:"pk;auto;column(id)"`
	Torrents []*Torrent `orm:"reverse(many)"`
	Name     string     `orm:"type(text)"`
}

type Uploader struct {
	ID   int64  `orm:"pk;auto;column(id)"`
	Name string `orm:"type(text)"`
}

var dblock sync.Mutex

func parseTags(tagSelector *goquery.Selection) (tags []*Tag) {
	for i := range tagSelector.Nodes {
		tag := Tag{
			Name: tagSelector.Eq(i).Text(),
		}

		// find or insert tag
		err := orm.Read(&tag, "Name")
		if err != nil {
			dblock.Lock()
			err = orm.Read(&tag, "Name")
			if err != nil {
				if debug {
					log.Printf("Adding tag: %s\n", tag.Name)
				}
				_, err = orm.Insert(&tag)
			}
			dblock.Unlock()
		}
		if err != nil {
			log.Fatalf("Can't insert tag %#v: %s\n", tag, err)
		}
		tags = append(tags, &tag)
	}
	return
}

func main() {

	//debug = true
	InitDatabase()

	// Instantiate default collector
	c := colly.NewCollector()

	// Before making a request print "Visiting ..."
	if debug {
		c.OnRequest(func(r *colly.Request) {
			log.Println("Visiting", r.URL.String())
		})
	}

	jobChan := make(chan bool, 20)
	// Only match on #searchResult, a table
	c.OnHTML("#searchResult tbody", func(e *colly.HTMLElement) {
		// Loop over each row
		url, _ := e.DOM.Find("div.detName a:first-of-type").Attr("href")
		urlParts := strings.Split(url, "/")
		//recent, _ := strconv.ParseInt(urlParts[2], 10, 32)
		max, _ := strconv.ParseInt(urlParts[2], 10, 64)
		for id := max; id > 0; id-- {
			t := Torrent{ID: id}
			err := orm.Read(&t, "ID")
			// If this one is already index
			if err == nil {
				// If the id is still recent ish, keep looking, to find what
				// looks like IDs claimed, but not available yet.
				if time.Now().After(t.Uploaded.Add(time.Hour * 24)) {
					continue
				}
				// This is is pretty old, assume nothing older than this is
				// going to show up.
				log.Println("All caught up with recent torrents")
				break
			}
			idStr := strconv.FormatInt(id, 10)
			jobChan <- true
			if debug {
				log.Printf("Starting job %d\n", id)
			}
			go c.Visit("http://uj3wazyk5u4hnvtk.onion/torrent/" + idStr)
		}
	})

	c.OnResponse(func(r *colly.Response) {
		<-jobChan
		if debug {
			log.Printf("Done with: %d\n", r.StatusCode)
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		if err.Error() == "Not Found" {
			<-jobChan
			if debug {
				log.Printf("%s not found\n", r.Request.URL)
			}
			return
		}
		if r.Request.URL.String() == "http://uj3wazyk5u4hnvtk.onion/recent" {
			log.Printf("Unable to load recent page\n")
			<-jobChan
			return
		}
		if debug {
			log.Printf("Got an error: %s retrying\n", err)
		}
		c.Visit(r.Request.URL.String())
	})

	c.OnHTML("#detailsouterframe", func(e *colly.HTMLElement) {
		var err error
		url := e.Request.URL
		idStr := (strings.Split(url.String(), "/"))[4]
		id, _ := strconv.ParseInt(idStr, 10, 64)
		t := Torrent{ID: id}

		title := e.DOM.Find("#title:first-of-type").Text()
		title = strings.TrimSpace(title)
		t.Title = title

		col := "col1"
		infoNodes := e.DOM.Find("#details ." + col).Clone()
		t.InfoHash = strings.TrimSpace(infoNodes.Children().Remove().End().Text())
		if t.InfoHash == "" {
			col := "col2"
			infoNodes := e.DOM.Find("#details ." + col).Clone()
			t.InfoHash = strings.TrimSpace(infoNodes.Children().Remove().End().Text())
		}
		if t.InfoHash == "" {
			log.Fatal("Can't get info hash, this is gonna be a bad time")
		}

		complete := true
		fieldNodes := e.DOM.Find("#details dt")
		valueNodes := e.DOM.Find("#details dd")
		for i := range fieldNodes.Nodes {
			field := fieldNodes.Eq(i).Text()
			value := valueNodes.Eq(i)
			if field == "Type:" {
				category, _ := value.Find(" a").Attr("href")
				categoryid, err := strconv.ParseInt((strings.Split(category, "/"))[2], 10, 32)
				// Find category in database
				if err != nil {
					log.Fatal("Couldn't parse the int out of this category: ", category, " ", url)
				}
				t.Category = &Category{
					ID:   categoryid,
					Name: value.Text(),
				}
				err = orm.Read(t.Category)
				if err != nil {
					dblock.Lock()
					err = orm.Read(t.Category)
					if err != nil {
						if debug {
							log.Printf("Adding category: %s\n", t.Category.Name)
						}
						_, err = orm.Insert(t.Category)
					}
					dblock.Unlock()
					if err != nil {
						log.Fatalf("Couldn't find or add category from %s %s %#v\n", url, err, t.Category)
					}
				}
			} else if field == "Files:" {
				t.Files, err = strconv.ParseInt(value.Text(), 10, 64)
			} else if field == "Size:" {
				sizeStr := (strings.Split(value.Text(), "("))[1]
				sizeStr = strings.TrimRight(sizeStr, "Bytes)")
				sizeStr = strings.TrimSpace(sizeStr)
				t.Size, err = strconv.ParseInt(sizeStr, 10, 64)
				if err != nil {
					log.Fatalf("Unable to determine size from: %s\n", value.Text())
				}
			} else if field == "Info Hash:" {
			} else if field == "Seeders:" {
			} else if field == "Leechers:" {
			} else if field == "Comments" {
			} else if field == "Tag(s):" {
				t.Tags = parseTags(value.Find("a"))
			} else if field == "Uploaded:" {
				t.Uploaded, err = time.Parse("2006-01-02 15:04:05 MST", value.Text())
			} else if field == "By:" {
				uploader := strings.TrimSpace(value.Text())
				t.Uploader = &Uploader{
					Name: uploader,
				}
				err = orm.Read(t.Uploader, "Name")
				if err != nil {
					dblock.Lock()
					err = orm.Read(t.Uploader, "Name")
					if err != nil {
						if debug {
							log.Printf("Adding new uploader: %s\n", uploader)
						}
						_, err = orm.Insert(t.Uploader)
					}
					dblock.Unlock()
				}
				if err != nil {
					log.Fatal("Couldn't find or add uploader: ", uploader)
				}
			} else if field == "Info:" {
				t.InfoURL, _ = value.Find("a").Attr("href")
			} else if field == "Spoken language(s):" {
				t.LangSpoken = strings.TrimSpace(value.Text())
			} else if field == "Texted language(s):" {
				t.LangTexted = strings.TrimSpace(value.Text())
			} else {
				fmt.Printf("Need to handle Field: [%s][%s]\n", field, value.Text())
				complete = false
			}
		}
		if !complete {
			log.Fatal("Deal with missing fields: %s\n", url)
		}
		dblock.Lock()
		_, err = orm.Insert(&t)
		dblock.Unlock()
		if err != nil {
			fmt.Printf("Error inserting torrent: %d %s\n", id, err)
		}
		m2m := orm.QueryM2M(&t, "Tags")
		for _, t := range t.Tags {
			dblock.Lock()
			m2m.Add(t)
			dblock.Unlock()
		}
		log.Printf("Indexed %d: %s\n", t.ID, t.Title)
	})

	rp, err := proxy.RoundRobinProxySwitcher("socks5://127.0.0.1:9050")
	if err != nil {
		log.Fatal(err)
	}
	c.SetProxyFunc(rp)

	// Don't flood the server, but keep sqlite3 busy
	//c.Limit(&colly.LimitRule{DomainGlob: "*", Parallelism: 5})

	// Allow fetching the recent feed multiple times
	c.AllowURLRevisit = true
	for {
		jobChan <- true
		var res []beegoorm.Params
		_, err := orm.Raw("SELECT count(id) AS count FROM torrent").Values(&res)
		if err != nil {
			log.Fatal("Can't get total number of torrents")
		}
		log.Printf("Checking for new torrents. %s torrents indexed. Jobs %d\n",
			res[0]["count"],
			len(jobChan),
		)
		go c.Visit("http://uj3wazyk5u4hnvtk.onion/recent")
		time.Sleep(time.Minute)
	}
	//c.Visit("http://uj3wazyk5u4hnvtk.onion/torrent/7842871")

	// Wait for all of the connections to finish
	c.Wait()
}
