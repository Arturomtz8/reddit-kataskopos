package reddit_kataskopos

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
)

const (
	searchCommand    string = "/search"
	telegramTokenEnv string = "GITHUB_BOT_TOKEN"
	defaultPostsLen  int    = 5
	timesToRecurse   int    = 10
)

const templ = `
  Title: {{.Title}}
  Link: {{.Link}}
  ⭐: {{.Ups}}
`

type JSONResponse struct {
	Data Data `json:"data"`
}

type Data struct {
	Children []PostSlice `json:"children"`
	Offset   string      `json:"after"`
}

type PostSlice struct {
	Data PostData `json:"data"`
}

type PostData struct {
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

type Update struct {
	UpdateID int     `json:"update_id"`
	Message  Message `json:"message"`
}

type Message struct {
	Text string `json:"text"`
	Chat Chat   `json:"chat"`
}

type Chat struct {
	Id int `json:"id"`
}

// the slice that will hold the recursive calls
var childrenSliceRecursive []PostSlice

func init() {
	functions.HTTP("HandleTelegramWebhook", HandleTelegramWebhook)
}

func HandleTelegramWebhook(_ http.ResponseWriter, r *http.Request) {
	update, err := parseTelegramRequest(r)
	if err != nil {
		sendTextToTelegramChat(update.Message.Chat.Id, err.Error())
		log.Printf("error: %s", err.Error())
		return
	}
	switch {
	case strings.HasPrefix(update.Message.Text, searchCommand):
		sanitizedString, err := sanitize(update.Message.Text, searchCommand)
		if err != nil {
			sendTextToTelegramChat(update.Message.Chat.Id, err.Error())
			log.Printf("error: %s", err.Error())
			return
		}

		responseFunc, err := postIt(sanitizedString, update.Message.Chat.Id)
		if err != nil {
			sendTextToTelegramChat(update.Message.Chat.Id, err.Error())
			log.Printf("error: %s", err.Error())
			return
		}
		log.Printf("successfully distributed to chat id %d, response from loop: %s", update.Message.Chat.Id, responseFunc)
		return

	default:
		log.Print("invalid command")
		sendTextToTelegramChat(update.Message.Chat.Id, "use /search {subreddit}, e.g: /search python")
		return
	}

}

func parseTelegramRequest(r *http.Request) (*Update, error) {
	var update Update
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		log.Printf("could not decode incoming update %s", err.Error())
		return nil, err
	}
	return &update, nil
}

func sanitize(s, botCommand string) (string, error) {
	var lenBotCommand int = len(botCommand)
	if len(s) >= lenBotCommand {
		if s[:lenBotCommand] == botCommand {
			s = s[lenBotCommand:]
			s = strings.TrimSpace(s)
			log.Printf("sanitized string is: %s\n", s)
			log.Printf("type of value entered: %T\n", s)
		}
	} else {
		return "", errors.New("invalid value: you must enter /search {subreddit}")
	}
	return s, nil

}

func postIt(subreddit string, chatId int) (string, error) {
	postsSlice, err := getPosts(subreddit)
	if err != nil {
		return "", err
	}
	responseFunc, err := shufflePostsAndSend(&postsSlice, chatId)
	if err != nil {
		return "", err
	}
	return responseFunc, err

}

func getPosts(subreddit string) ([]Post, error) {
	var postsSlice []Post
	currentTime := time.Now()
	lastTwoMonths := currentTime.AddDate(0, 0, -60)

	childrenSlice, err := makeRequest(subreddit, "no", timesToRecurse)
	if err != nil {
		return nil, err
	}
	log.Println("slice len of children", len(childrenSlice))
	// if len(childrenSlice) == 0 {
	// 	err := errors.New("No posts found in request to subreddit")
	// 	return nil, err
	// }
	for _, child := range childrenSlice {
		postScore := child.Data.Ups
		createdDateUnix := child.Data.Created
		createdDate := time.Time(time.Unix(int64(createdDateUnix), 0))

		if postScore >= 25 && inTimeSpan(lastTwoMonths, currentTime, createdDate) {
			log.Println(createdDate)
			child.Data.Link = "https://reddit.com" + child.Data.Link

			post := Post{Ups: child.Data.Ups,
				Title: child.Data.Title,
				Link:  child.Data.Link,
			}
			postsSlice = append(postsSlice, post)
		}
	}
	if len(postsSlice) == 0 {
		err := errors.New("No interesting posts in subreddit")
		return nil, err
	}
	return postsSlice, nil
}

func makeRequest(subreddit, after string, iteration int) ([]PostSlice, error) {
	var jsonResponse JSONResponse
	var subreddit_url string

	if iteration == timesToRecurse {
		subreddit_url = fmt.Sprintf("https://old.reddit.com/r/%s/.json?limit=100", subreddit)
	} else if iteration > 0 {
		jsonResponse.Data.Offset = after
		subreddit_url = fmt.Sprintf("https://old.reddit.com/r/%s/.json?limit=100&after=%s", subreddit, jsonResponse.Data.Offset)
	} else {
		return childrenSliceRecursive, nil
	}

	log.Println("number of iteration", iteration)
	client := &http.Client{}
	req, err := http.NewRequest("GET", subreddit_url, nil)
	if err != nil {
		return childrenSliceRecursive, err
	}

	req.Header.Set("User-Agent", after)
	resp, err := client.Do(req)
	if err != nil {
		return childrenSliceRecursive, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return childrenSliceRecursive, err
	}

	err = json.Unmarshal(body, &jsonResponse)
	if err != nil {
		return childrenSliceRecursive, err
	}

	if len(jsonResponse.Data.Children) == 0 {
		return childrenSliceRecursive, errors.New("No interesting posts in subreddit")
	}

	for i := range jsonResponse.Data.Children {
		childrenOnly := jsonResponse.Data.Children[i]
		childrenSliceRecursive = append(childrenSliceRecursive, childrenOnly)
	}

	resp.Body.Close()
	makeRequest(subreddit, jsonResponse.Data.Offset, iteration-1)
	return childrenSliceRecursive, nil

}

func inTimeSpan(lastTwoMonths, currentTime, check time.Time) bool {
	return check.After(lastTwoMonths) && check.Before(currentTime)
}

func shufflePostsAndSend(postsArrayPointer *[]Post, chatId int) (string, error) {
	var postsLen int
	// shuffle data
	postsArray := *postsArrayPointer
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(postsArray), func(i, j int) { postsArray[i], postsArray[j] = postsArray[j], postsArray[i] })

	newSlice := make([]string, 0)

	if len(postsArray) < defaultPostsLen {
		postsLen = len(postsArray)
	} else {
		postsLen = defaultPostsLen
	}
	for i := 0; i < postsLen; i++ {
		post := postsArray[i]
		var report = template.Must(template.New("subrredits").Parse(templ))
		buf := &bytes.Buffer{}
		if err := report.Execute(buf, post); err != nil {
			return "", err
		}
		s := buf.String()
		newSlice = append(newSlice, s)
	}
	textPosts := strings.Join(newSlice, "\n-------------\n")
	textPosts = html.UnescapeString(textPosts)
	// log.Println(textPosts)
	responseFunc, err := sendTextToTelegramChat(chatId, textPosts)
	if err != nil {
		return "", err
	}
	return responseFunc, nil
}

func sendTextToTelegramChat(chatId int, text string) (string, error) {
	log.Printf("sending %s to chat_id: %d \n", text, chatId)

	var telegramApi string = "https://api.telegram.org/bot" + os.Getenv("GITHUB_BOT_TOKEN") + "/sendMessage"

	response, err := http.PostForm(
		telegramApi,
		url.Values{
			"chat_id": {strconv.Itoa(chatId)},
			"text":    {text},
		})
	if err != nil {
		log.Printf("error when posting text to the chat: %s", err.Error())
		return "", err
	}
	defer response.Body.Close()
	var bodyBytes, errRead = ioutil.ReadAll(response.Body)
	if errRead != nil {
		log.Printf("error parsing telegram answer %s", errRead.Error())
		return "", err
	}

	bodyString := string(bodyBytes)
	log.Printf("body of telegram response: %s \n", bodyString)
	return bodyString, nil

}
