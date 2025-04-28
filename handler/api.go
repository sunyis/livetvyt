package handler

import (
	"embed"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	freq "github.com/imroc/req/v3"
	"github.com/snowie2000/livetv/global"
	"github.com/snowie2000/livetv/model"
	"github.com/snowie2000/livetv/plugin"
	"github.com/snowie2000/livetv/recaptcha"
	"github.com/snowie2000/livetv/service"
	"github.com/snowie2000/livetv/util"

	"golang.org/x/text/language"
)

var langMatcher = language.NewMatcher([]language.Tag{
	language.English,
	language.Chinese,
})

//go:embed web
var webFS embed.FS

/** fetch web content as a browser*/
func FetchHandler(c *gin.Context) {
	disableProtection := os.Getenv("LIVETV_FREEACCESS") == "1"
	// verify token against the unique token of the requested channel
	if !disableProtection {
		token := c.Query("token")
		if token != global.GetSecretToken() { // invalid token
			c.String(http.StatusForbidden, "Forbidden")
			return
		}
	}

	url := c.Query("url")
	if url == "" {
		c.AbortWithStatus(404)
		return
	}
	device := c.Query("device")
	if device == "" {
		device = "chrome"
	}

	client := freq.C()
	switch device {
	case "safari":
		client.ImpersonateSafari()
	case "firefox":
		client.ImpersonateFirefox()
	case "iphone":
		client.
			ImpersonateSafari().
			SetTLSFingerprintIOS().
			SetCommonHeaders(map[string]string{
				"accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
				"sec-fetch-site":  "same-origin",
				"sec-fetch-dest":  "document",
				"accept-language": "zh-CN,zh-Hans;q=0.9",
				"sec-fetch-mode":  "navigate",
				"user-agent":      "Mozilla/5.0 (iPhone; CPU iPhone OS 14_0 like Mac OS X) AppleWebKit/603.1.30 (KHTML, like Gecko) Version/12.0.0 Mobile/15A5370a Safari/602.1",
			})
	case "ipad":
		client.
			ImpersonateSafari().
			SetTLSFingerprintIOS().
			SetCommonHeaders(map[string]string{
				"accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
				"sec-fetch-site":  "same-origin",
				"sec-fetch-dest":  "document",
				"accept-language": "zh-CN,zh-Hans;q=0.9",
				"sec-fetch-mode":  "navigate",
				"user-agent":      "Mozilla/5.0 (iPad; CPU iPhone OS 14_3 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0.2 Mobile/15E148 Safari/604.1",
			})
	case "android":
		client.
			ImpersonateChrome().
			SetTLSFingerprintAndroid().
			SetCommonHeaders(map[string]string{
				"pragma":                    "no-cache",
				"cache-control":             "no-cache",
				"sec-ch-ua":                 `"Not/A)Brand";v="8", "Chromium";v="126", "Google Chrome";v="126"`,
				"sec-ch-ua-mobile":          "?1",
				"sec-ch-ua-platform":        `"Android"`,
				"upgrade-insecure-requests": "1",
				"user-agent":                "Mozilla/5.0 (Linux; Android 8.0.0; SM-G955U Build/R16NW) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Mobile Safari/537.36",
				"accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
				"sec-fetch-site":            "same-origin",
				"sec-fetch-mode":            "navigate",
				"sec-fetch-user":            "?1",
				"sec-fetch-dest":            "document",
				"accept-language":           "zh-CN,zh;q=0.9,en;q=0.8,zh-TW;q=0.7,it;q=0.6",
			})
	default:
		client.ImpersonateChrome()
	}

	req := client.R().SetBody(c.Request.Body)
	headers := c.Request.Header
	for key, value := range headers {
		req.SetHeader(key, value[0])
	}
	req.RawURL = url
	req.Method = c.Request.Method

	resp := req.Do()
	for k, v := range resp.Header {
		if len(v) > 0 {
			c.Header(k, v[0])
		}
	}
	c.Writer.WriteHeader(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)
}

func CaptchaHandler(c *gin.Context) {
	if sessions.Default(c).Get("logined") == true {
		c.String(http.StatusOK, "{}") // do not generate captcha for loggin users
		return
	}
	captcha, err := recaptcha.DefaultCaptcha.GenerateCaptcha()
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, captcha)
}

func IndexHandler(c *gin.Context) {
	fullPath := strings.ReplaceAll(filepath.Join("web", c.Param("path")), "\\", "/")

	// Check if file exists
	f, err := webFS.Open(fullPath)
	if f != nil {
		fi, _ := f.Stat()
		if fi.IsDir() {
			err = errors.New("can't serve a folder")
			f.Close()
		}
	}
	if err != nil {
		// Not found, serve index.html
		fullPath = strings.ReplaceAll(filepath.Join("web", "index.html"), "\\", "/")
		f, err = webFS.Open(fullPath)
	}

	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	defer f.Close()
	c.Writer.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(fullPath)))
	io.Copy(c.Writer, f)
}

func loadConfig() (Config, error) {
	var conf Config
	if cmd, err := global.GetConfig("ytdl_cmd"); err == nil {
		conf.Cmd = cmd
	}
	if args, err := global.GetConfig("ytdl_args"); err == nil {
		conf.Args = args
	}
	if burl, err := global.GetConfig("base_url"); err == nil {
		conf.BaseURL = burl
	}
	if secret, err := global.GetConfig("secret"); err == nil {
		conf.Secret = secret
	}
	if apiKey, err := global.GetConfig("apiKey"); err == nil {
		conf.ApiKey = apiKey
	}
	return conf, nil
}

func CRSFHandler(c *gin.Context) {
	session := sessions.Default(c)
	// session.Options(sessions.Options{
	// 	SameSite: http.SameSiteNoneMode,
	// 	Secure:   true,
	// })
	crsfToken := util.RandString(10)
	session.Set("crsfToken", crsfToken)
	err := session.Save()
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
	} else {
		c.Data(http.StatusOK, "text/plain", []byte(crsfToken))
	}
}

func PluginListHandler(c *gin.Context) {
	if sessions.Default(c).Get("logined") != true {
		c.String(http.StatusUnauthorized, "Unauthorized")
		return
	}
	list := plugin.GetPluginList()
	c.JSON(http.StatusOK, list)
}

func ChannelListHandler(c *gin.Context) {
	if sessions.Default(c).Get("logined") != true {
		c.String(http.StatusUnauthorized, "Unauthorized")
		return
	}
	baseUrl, err := global.GetConfig("base_url")
	if err != nil {
		log.Println(err.Error())
		c.String(http.StatusInternalServerError, "error: %s", err.Error())
		return
	}
	channelModels, err := service.GetAllChannel()
	if err != nil {
		log.Println(err.Error())
		c.String(http.StatusInternalServerError, "error: %s", err.Error())
		return
	}
	channels := make([]Channel, len(channelModels)+1)
	channels[0] = Channel{
		ID:     "0",
		Name:   "playlist",
		M3U8:   fmt.Sprintf("%s/lives.m3u?token=%s", baseUrl, global.GetSecretToken()),
		Status: service.Ok,
	}
	for i, v := range channelModels {
		status := service.GetStatus(v.URL)
		ch := Channel{
			ID:         v.ChannelID,
			Name:       v.Name,
			URL:        v.URL,
			Parser:     v.Parser,
			TsProxy:    v.TsProxy,
			M3U8:       fmt.Sprintf("%s/live.m3u8?token=%s&c=%d", baseUrl, v.Token, v.ID),
			Proxy:      v.Proxy,
			ProxyUrl:   v.ProxyUrl,
			LastUpdate: status.Time.Format("2006-01-02 15:04:05"),
			Status:     status.Status,
			Message:    status.Msg,
			Category:   v.Category,
		}
		if len(v.Children) > 0 {
			list := []Channel{}
			for _, sub := range v.Children {
				status := service.GetStatus(sub.URL)
				subID := fmt.Sprintf("%d-%d", v.ID, sub.ID)
				c := Channel{
					ID:         sub.ChannelID,
					Name:       sub.Name,
					URL:        sub.URL,
					Parser:     sub.Parser,
					TsProxy:    sub.TsProxy,
					M3U8:       fmt.Sprintf("%s/live.m3u8?token=%s&c=%s", baseUrl, sub.Token, subID),
					Proxy:      sub.Proxy,
					ProxyUrl:   sub.ProxyUrl,
					LastUpdate: status.Time.Format("2006-01-02 15:04:05"),
					Status:     status.Status,
					Message:    status.Msg,
					Category:   sub.Category,
					Virtual:    true, // sub channels are all virtual
				}
				list = append(list, c)
			}
			ch.Children = list
		}
		channels[i+1] = ch
	}
	c.JSON(http.StatusOK, channels)
}

func NewChannelHandler(c *gin.Context) {
	if sessions.Default(c).Get("logined") != true {
		c.String(http.StatusUnauthorized, "Unauthorized")
		return
	}
	chName := c.PostForm("name")
	chURL := c.PostForm("url")
	chParser := c.PostForm("parser")
	chProxyUrl := c.PostForm("proxyurl")
	chTsProxy := c.PostForm("tsproxy")
	chCategory := c.PostForm("category")
	if chName == "" || chURL == "" {
		c.String(http.StatusBadRequest, "Incomplete channel info")
		return
	}
	chProxy := c.PostForm("proxy") == "true"
	mch := &model.Channel{
		Name:          chName,
		URL:           chURL,
		Proxy:         chProxy,
		ProxyUrl:      chProxyUrl,
		Parser:        chParser,
		TsProxy:       chTsProxy,
		Category:      chCategory,
		HasSubChannel: false,
	}
	// check if the parser can provide sub channels
	if p, err := plugin.GetPlugin(chParser); err == nil {
		if _, ok := p.(plugin.ChannalProvider); ok {
			mch.HasSubChannel = true
		}
	}
	err := service.SaveChannel(mch)
	if err != nil {
		log.Println(err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.String(http.StatusOK, "")
	go service.UpdateURLCacheSingle(mch, true) // update liveURL on adding new channel
}

func AuthProbeHandler(c *gin.Context) {
	if sessions.Default(c).Get("logined") != true {
		c.String(http.StatusUnauthorized, "Unauthorized")
	} else {
		c.String(http.StatusOK, "")
	}
}

func UpdateChannelHandler(c *gin.Context) {
	if sessions.Default(c).Get("logined") != true {
		c.String(http.StatusUnauthorized, "Unauthorized")
		return
	}
	chID, chSubId := getChannelNumbers(c.PostForm("id"))
	if chID == 0 {
		c.String(http.StatusInternalServerError, "empty id")
		return
	}
	if chSubId >= 0 {
		c.String(http.StatusInternalServerError, "can't update sub channels")
		return
	}
	channel, err := service.GetChannel(chID, -1)
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}
	chName := c.PostForm("name")
	chURL := c.PostForm("url")
	chParser := c.PostForm("parser")
	chProxyUrl := c.PostForm("proxyurl")
	chTsProxy := c.PostForm("tsproxy")
	chCategory := c.PostForm("category")
	if chName == "" || chURL == "" {
		c.String(http.StatusBadRequest, "Incomplete channel info")
		return
	}
	chProxy := c.PostForm("proxy") == "true"
	channel.Name = chName
	channel.Parser = chParser
	channel.Proxy = chProxy
	channel.ProxyUrl = chProxyUrl
	channel.URL = chURL
	channel.TsProxy = chTsProxy
	channel.Category = chCategory
	channel.HasSubChannel = false

	// check if the parser can provide sub channels
	if p, err := plugin.GetPlugin(chParser); err == nil {
		if _, ok := p.(plugin.ChannalProvider); ok {
			channel.HasSubChannel = true
		}
	}
	err = service.SaveChannel(channel)
	if err != nil {
		log.Println(err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.String(http.StatusOK, "")
	go service.UpdateURLCacheSingle(channel, true) // update liveURL on updating new channel
}

func DeleteChannelHandler(c *gin.Context) {
	if sessions.Default(c).Get("logined") != true {
		c.String(http.StatusUnauthorized, "Unauthorized")
		return
	}
	chID, chSubId := getChannelNumbers(c.Query("id"))
	if chID == 0 {
		c.String(http.StatusInternalServerError, "empty id")
		return
	}
	if chSubId >= 0 {
		c.String(http.StatusInternalServerError, "can't delete sub channels")
		return
	}
	err := service.DeleteChannel(chID)
	if err != nil {
		log.Println(err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.String(http.StatusOK, "")
}

func GetConfigHandler(c *gin.Context) {
	if sessions.Default(c).Get("logined") != true {
		c.String(http.StatusUnauthorized, "Unauthorized")
		return
	}
	conf, err := loadConfig()
	if err != nil {
		log.Println(err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, conf)
}

func CategoryHandler(c *gin.Context) {
	if sessions.Default(c).Get("logined") != true {
		c.String(http.StatusUnauthorized, "Unauthorized")
		return
	}
	categories := global.GetAllCategories()
	c.JSON(http.StatusOK, categories)
}

func UpdateConfigHandler(c *gin.Context) {
	if sessions.Default(c).Get("logined") != true {
		c.String(http.StatusUnauthorized, "Unauthorized")
		return
	}
	ytdlCmd := c.PostForm("cmd")
	ytdlArgs := c.PostForm("args")
	baseUrl := strings.TrimSuffix(c.PostForm("baseurl"), "/")
	apiKey := strings.TrimSpace(c.PostForm("apikey"))
	secret := strings.TrimSpace(c.PostForm("secret"))
	if len(ytdlCmd) > 0 {
		err := global.SetConfig("ytdl_cmd", ytdlCmd)
		if err != nil {
			log.Println(err.Error())
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
	}
	if len(ytdlArgs) > 0 {
		err := global.SetConfig("ytdl_args", ytdlArgs)
		if err != nil {
			log.Println(err.Error())
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
	}
	if len(baseUrl) > 0 {
		err := global.SetConfig("base_url", baseUrl)
		if err != nil {
			log.Println(err.Error())
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
	}
	global.SetConfig("apiKey", apiKey)
	global.SetConfig("secret", secret)
	global.ClearSecretToken()
	c.String(http.StatusOK, "")
}

func LogHandler(c *gin.Context) {
	if sessions.Default(c).Get("logined") != true {
		c.String(http.StatusUnauthorized, "Unauthorized")
		return
	}
	c.File(os.Getenv("LIVETV_DATADIR") + "/livetv.log")
}

func LoginViewHandler(c *gin.Context) {
	session := sessions.Default(c)
	crsfToken := util.RandString(10)
	session.Set("crsfToken", crsfToken)
	err := session.Save()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{
			"ErrMsg": err.Error(),
		})
		return
	}
	c.HTML(http.StatusOK, "login.html", gin.H{
		"Crsf": crsfToken,
	})
}

func LoginActionHandler(c *gin.Context) {
	session := sessions.Default(c)
	// session.Options(sessions.Options{
	// 	SameSite: http.SameSiteNoneMode,
	// 	Secure:   true,
	// })
	crsfToken := c.PostForm("crsf")
	if crsfToken != session.Get("crsfToken") {
		log.Println(crsfToken, session.Get("crsfToken"))
		c.String(http.StatusBadRequest, "bad request")
		return
	}
	// verify captcha before verifying password so as to protect us from bruteforce attack.
	captchaId := c.PostForm("captcha_id")
	captchaAnswer := c.PostForm("answer")
	if !recaptcha.DefaultCaptcha.Verify(&recaptcha.CaptchaData{captchaId, "", captchaAnswer}) {
		c.String(http.StatusForbidden, "Invalid captcha")
		return
	}
	pass := c.PostForm("password")
	cfgPass, err := global.GetConfig("password")
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if pass == cfgPass {
		session.Set("logined", true)
		err = session.Save()
		if err != nil {
			log.Println(err.Error())
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.String(http.StatusOK, "ok")
	} else {
		c.String(http.StatusForbidden, "Password error!")
	}
}

func LogoutHandler(c *gin.Context) {
	if sessions.Default(c).Get("logined") != true {
		c.String(http.StatusOK, "")
		return
	}
	session := sessions.Default(c)
	session.Delete("logined")
	err := session.Save()
	if err != nil {
		log.Println(err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.String(http.StatusOK, "")
}

func CORSHandler(c *gin.Context) {
	if strings.HasPrefix(c.Request.URL.Path, "/api") {
		c.Status(http.StatusForbidden)
		return
	}
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	c.Writer.Header().Set("Access-Control-Allow-Methods", "*")
	c.Status(http.StatusOK)
}

func ChangePasswordHandler(c *gin.Context) {
	if sessions.Default(c).Get("logined") != true {
		c.String(http.StatusUnauthorized, "Unauthorized")
		return
	}
	pass := c.PostForm("password")
	pass2 := c.PostForm("password2")
	if pass == "" {
		c.String(http.StatusBadRequest, "Empty password!")
	}
	if pass != pass2 {
		c.String(http.StatusBadRequest, "Password mismatch!")
	}
	err := global.SetConfig("password", pass)
	if err != nil {
		log.Println(err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	LogoutHandler(c)
}

func init() {
	mime.AddExtensionType(".ts", "video/mp2t")
}
