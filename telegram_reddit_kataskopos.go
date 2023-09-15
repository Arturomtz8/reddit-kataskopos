package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
)

type FirstJSONLevel struct {
	Data SecondJSONLevel `json:"data"`
}

type SecondJSONLevel struct {
	Children []ThirdJSONLevel `json:"children"`
}

type ThirdJSONLevel struct {
	Data FinalJSONLevel `json:"data"`
}

type FinalJSONLevel struct {
	Ups     int     `json:"ups"`
	Title   string  `json:"title"`
	Link    string  `json:"permalink"`
	Created float64 `json:"created"`
}

type Post struct {
	Ups   int
	Title string
	Link  string
}

func main() {
	currentTime := time.Now()

	stringCurrentTime := currentTime.Format("2006-01-02")
	arg := os.Args[1]

	jsonResponse := makeRequest(arg)
	slicePosts := parseJson(jsonResponse, stringCurrentTime)
	shufflePosts(slicePosts)

}

func makeRequest(subreddit string) FirstJSONLevel {
	client := &http.Client{}
	subreddit_url := fmt.Sprintf("https://old.reddit.com/r/%s/.json?limit=100", subreddit)
	req, err := http.NewRequest("GET", subreddit_url, nil)

	if err != nil {
		panic(err)
	}

	req.Header.Set("User-Agent", "bla")

	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}

	body, err := ioutil.ReadAll(resp.Body)

	defer resp.Body.Close()

	var jsonResponse FirstJSONLevel

	err = json.Unmarshal(body, &jsonResponse)
	if err != nil {
		log.Fatal(err)
	}
	return jsonResponse

}

func parseJson(jsonResponse FirstJSONLevel, today string) []Post {
	var postsArray []Post

	for i := range jsonResponse.Data.Children {
		postScore := jsonResponse.Data.Children[i].Data.Ups
		createdDate := jsonResponse.Data.Children[i].Data.Created
		createdDateClean := string(time.Time(time.Unix(int64(createdDate), 0)).Format(time.RFC3339))
		createdDateClean = strings.Split(createdDateClean, "T")[0]
		if createdDateClean == today && postScore >= 150 {
			jsonResponse.Data.Children[i].Data.Link = "https://reddit.com" + jsonResponse.Data.Children[i].Data.Link

			post := Post{Ups: jsonResponse.Data.Children[i].Data.Ups,
				Title: jsonResponse.Data.Children[i].Data.Title,
				Link:  jsonResponse.Data.Children[i].Data.Link,
			}
			postsArray = append(postsArray, post)
		}
	}
	return postsArray
}

func shufflePosts(postsArray []Post) string {
	// shuffle data
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(postsArray), func(i, j int) { postsArray[i], postsArray[j] = postsArray[j], postsArray[i] })

	for i := 1; i <= 5; i++ {
		post := postsArray[i]
		jsonIndented, err := json.MarshalIndent(post, "", "\t")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("Selected post -> ", string(jsonIndented))
	}
	return "finished"
}
