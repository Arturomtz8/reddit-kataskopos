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
)

const templ = `
  Title: {{.Title}}
  Link: {{.Link}}
  ⭐: {{.Ups}}
`

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
	currentTime := time.Now()
	lastSevenDays := currentTime.AddDate(0, 0, -7)

	jsonResponse, err := makeRequest(subreddit)
	if err != nil {
		return "", err
	}
	slicePosts, err := parseJson(&jsonResponse, lastSevenDays, currentTime)
	if err != nil {
		return "", err
	}
	responseFunc, err := shufflePostsAndSend(&slicePosts, chatId)
	if err != nil {
		return "", err
	}
	return responseFunc, nil

}

func makeRequest(subreddit string) (FirstJSONLevel, error) {
	var jsonResponse FirstJSONLevel

	client := &http.Client{}
	subreddit_url := fmt.Sprintf("https://old.reddit.com/r/%s/.json?limit=100", subreddit)
	req, err := http.NewRequest("GET", subreddit_url, nil)

	if err != nil {
		log.Printf("error: %s", err.Error())
		return FirstJSONLevel{}, err
	}

	req.Header.Set("User-Agent", "bla")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("error: %s", err.Error())
		return FirstJSONLevel{}, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("error: %s", err.Error())
		return FirstJSONLevel{}, err
	}

	defer resp.Body.Close()

	err = json.Unmarshal(body, &jsonResponse)
	if err != nil {
		log.Printf("error: %s", err.Error())
		return FirstJSONLevel{}, err
	}
	return jsonResponse, nil

}

func parseJson(jsonResponse *FirstJSONLevel, lastSevenDays, currentTime time.Time) ([]Post, error) {
	var postsArray []Post

	if len(jsonResponse.Data.Children) == 0 {
		err := errors.New("Not enough posts in subreddit")
		log.Printf("error: %s", err.Error())
		return nil, err
	}

	for i := range jsonResponse.Data.Children {
		postScore := jsonResponse.Data.Children[i].Data.Ups
		createdDateUnix := jsonResponse.Data.Children[i].Data.Created
		createdDate := time.Time(time.Unix(int64(createdDateUnix), 0))

		if postScore >= 50 && inTimeSpan(lastSevenDays, currentTime, createdDate) {
			log.Println(createdDate)
			jsonResponse.Data.Children[i].Data.Link = "https://reddit.com" + jsonResponse.Data.Children[i].Data.Link

			post := Post{Ups: jsonResponse.Data.Children[i].Data.Ups,
				Title: jsonResponse.Data.Children[i].Data.Title,
				Link:  jsonResponse.Data.Children[i].Data.Link,
			}
			postsArray = append(postsArray, post)
			log.Println(postsArray)
		}
	}
	return postsArray, nil
}

func inTimeSpan(lastSevenDays, currentTime, check time.Time) bool {
	return check.After(lastSevenDays) && check.Before(currentTime)
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
	log.Println(textPosts)
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
