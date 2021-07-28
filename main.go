package main

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"

	"gopkg.in/yaml.v3"
)

var remote *url.URL
var SuccessfulRequest int
var ErroredRequest int

//go:embed templates/*
var f embed.FS

type ConfigFile struct {
	Email    string `yaml:"email"`
	Domain   string `yaml:"domain"`
	Services []struct {
		Host      string `yaml:"host"`
		Port      int    `yaml:"port"`
		Subdomain string `yaml:"subdomain"`
	} `yaml:"services"`
}

type Page struct {
	Title             string
	Message           string
	Code              int
	TotalRequest      int
	SuccessfulRequest int
	ErroredRequest    int
}

func init() {
	gin.SetMode(gin.ReleaseMode)

	fmt.Println("Proxit Started")
	fmt.Println("Main Domain : " + GetConfig().Domain)
	fmt.Println("Total Service : " + strconv.Itoa((len(GetConfig().Services))))
	SuccessfulRequest = 0
	ErroredRequest = 0
}

func main() {

	proxit := gin.New()

	templ := template.Must(template.New("").ParseFS(f, "templates/*.tmpl"))
	proxit.SetHTMLTemplate(templ)

	proxit.Use(gin.Recovery())
	proxit.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return fmt.Sprintf("[ Proxit ]  %s - [%s] \"%s %s %s %d %s %s\"\n",
			param.ClientIP,
			param.TimeStamp.Format(time.RFC1123),
			param.Method,
			param.Request.Proto,
			param.Request.Host+param.Path,
			param.StatusCode,
			param.Latency,
			param.ErrorMessage,
		)
	}))
	proxit.Use(gzip.Gzip(gzip.BestCompression))
	proxit.NoRoute(CheckRequest)
	if GetConfig().Domain != "localhost" {

		var hosts []string
		for _, service := range GetConfig().Services {
			hosts = append(hosts, service.Subdomain+"."+GetConfig().Domain, GetConfig().Domain)
		}

		certmagic.DefaultACME.Agreed = true
		certmagic.DefaultACME.Email = GetConfig().Email
		certmagic.DefaultACME.CA = certmagic.LetsEncryptProductionCA
		err := certmagic.HTTPS(hosts, proxit)
		if err != nil {
			panic(err)
		}

		tlsConfig := certmagic.Default.TLSConfig()
		tlsConfig.NextProtos = append(tlsConfig.NextProtos, "h2")
		listener, _ := certmagic.Listen(hosts)

		srv := http.Server{
			ReadTimeout:       90 * time.Second,
			WriteTimeout:      90 * time.Second,
			IdleTimeout:       90 * time.Second,
			ReadHeaderTimeout: 90 * time.Second,
			Handler:           proxit,
			Addr:              ":443",
		}

		srv.Serve(listener)

	} else {

		proxit.Run(":80")
	}
}

func GetConfig() ConfigFile {
	var configs ConfigFile
	filename, _ := filepath.Abs("./services.yml")
	yamlFile, _ := os.ReadFile(filename)
	yaml.Unmarshal(yamlFile, &configs)
	return configs
}

func CheckRequest(c *gin.Context) {
	config := GetConfig()
	domainparts := strings.Split(c.Request.Host, ".")
	for _, service := range config.Services {
		if len(domainparts) < 2 {
			if service.Subdomain == "/" {
				Handle(c, service.Host, service.Port)
				return
			}
			DefaultPage(c)
			return
		} else {
			if domainparts[0] == service.Subdomain {
				Handle(c, service.Host, service.Port)
				return
			}
		}
	}
	ErrorPage(c, "Service Not Found", http.StatusNotFound)
}

func Handle(c *gin.Context, host string, port int) {
	domain := fmt.Sprintf("http://%v:%v", host, port)
	remote, _ = url.Parse(domain)

	c.Writer.WriteHeader(http.StatusOK)
	proxy := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			r.Header.Add("X-Forwarded-Host", c.Request.Host)
			r.Header.Add("X-Origin-Host", remote.Host)
			r.URL.Scheme = remote.Scheme
			r.URL.Host = remote.Host
		},
		ErrorHandler: func(rw http.ResponseWriter, r *http.Request, err error) {
			c.Writer.WriteHeader(http.StatusServiceUnavailable)
			ErrorPage(c, err.Error(), http.StatusServiceUnavailable)
		},
		ModifyResponse: func(r *http.Response) error {
			if r.Header.Get("Server") != "" {
				r.Header.Set("Server", "Proxit")
			} else {
				r.Header.Add("Server", "Proxit")
			}
			return nil
		},
	}
	proxy.ServeHTTP(c.Writer, c.Request)
	if c.Writer.Status() != 500 && c.Writer.Status() != 404 {
		SuccessfulRequest++
	}
}

func ErrorPage(c *gin.Context, message string, code int) {
	ErroredRequest++
	c.Header("Server", "Proxit")
	data := &Page{
		Code:    code,
		Message: message,
	}
	c.HTML(code, "error.tmpl", data)
}

func DefaultPage(c *gin.Context) {
	c.Header("Server", "Proxit")
	data := &Page{
		Title:             "Proxit Reverse Proxy",
		TotalRequest:      SuccessfulRequest + ErroredRequest,
		SuccessfulRequest: SuccessfulRequest,
		ErroredRequest:    ErroredRequest,
	}
	c.HTML(http.StatusOK, "index.tmpl", data)
}
