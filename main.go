package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	firebase "firebase.google.com/go/v4"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/go-pg/pg/v10/orm"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"google.golang.org/api/option"
)

const FILES = "./files"

// Number of items per page
const ITEMS = 20

type File struct {
	ID          uuid.UUID `json:"id" pg:",type:uuid"`
	UserID      string    `json:"user_id" pg:",notnull"`
	CreatedAt   time.Time `json:"created_at" pg:"default:now()"`
	Size        int64     `json:"size"`
	Filename    string    `json:"name"`
	ContentType string    `json:"type"`
	Location    string    `json:"location"`
}

var db *pg.DB
var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

type dbLogger struct{}

func (d dbLogger) BeforeQuery(c context.Context, q *pg.QueryEvent) (context.Context, error) {
	return c, nil
}

func (d dbLogger) AfterQuery(c context.Context, q *pg.QueryEvent) error {
	l, _ := q.FormattedQuery()
	fmt.Println(string(l))
	return nil
}

func main() {
	// Load env variables from .env
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	bucket := aws.String(os.Getenv("AWS_BUCKET"))

	// Initialize firebase app
	app, err := firebase.NewApp(context.Background(), nil, option.WithCredentialsFile("private_keys.json"))
	if err != nil {
		panic(err)
	}

	// Get firebase auth
	client, err := app.Auth(context.Background())
	if err != nil {
		panic(err)
	}

	// Initialize Amazon S3
	sess := session.Must(session.NewSession())
	svc := s3.New(sess)
	uploader := s3manager.NewUploader(sess)

	// Create router
	r := gin.Default()

	// Connect to DB
	db = pg.Connect(&pg.Options{
		User:     os.Getenv("POSTGRES_USERNAME"),
		Password: os.Getenv("POSTGRES_PASSWORD"),
	})
	defer db.Close()

	// Add logging to DB
	db.AddQueryHook(dbLogger{})

	// Define DB tables
	models := []interface{}{
		(*File)(nil),
	}

	// Create DB tables
	for _, model := range models {
		db.Model(model).DropTable(&orm.DropTableOptions{IfExists: true})
		err := db.Model(model).CreateTable(&orm.CreateTableOptions{
			IfNotExists: true,
		})
		if err != nil {
			panic(err)
		}
	}

	// Create index for uid
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_user_id ON files (user_id)`); err != nil {
		panic(err)
	}

	r.Use(static.Serve("/", static.LocalFile("static", false)))

	r.GET("/files", func(c *gin.Context) {
		p, err := strconv.Atoi(c.Query("page"))
		if err != nil {
			p = 0
		} else if p < 0 {
			p = 0
		}

		authToken, err := client.VerifyIDToken(context.Background(), c.GetHeader("Authorization"))
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		files := make([]File, 0)
		if err := db.Model(&files).
			Where("user_id = ?", authToken.UID).
			Order("created_at DESC").
			Offset(p * ITEMS).
			Limit(ITEMS).
			Select(); err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.JSON(http.StatusOK, files)
	})

	r.GET("/files/:id", func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		file := File{ID: id}

		if err := db.Model(&file).WherePK().Select(); err != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		output, err := svc.GetObject(&s3.GetObjectInput{
			Bucket: bucket,
			Key:    aws.String(file.ID.String()),
		})
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}

		c.DataFromReader(http.StatusOK, *output.ContentLength, file.ContentType, output.Body, map[string]string{
			"Content-Security-Policy": "default-src 'none'",
			"Content-Disposition":     "filename=\"" + quoteEscaper.Replace(file.Filename) + "\"",
		})
	})

	r.DELETE("/files/:id", func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		authToken, err := client.VerifyIDToken(context.Background(), c.GetHeader("Authorization"))
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		file := File{ID: id}
		if err := db.Model(&file).WherePK().Select(); err != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		if file.UserID == authToken.UID {
			if _, err := db.Model(&file).WherePK().Delete(); err != nil {
				c.AbortWithError(http.StatusInternalServerError, err)
				return
			}

			c.Status(http.StatusNoContent)
			_, err = svc.DeleteObject(&s3.DeleteObjectInput{
				Bucket: bucket,
				Key:    aws.String(file.ID.String()),
			})
			if err != nil {
				log.Printf("failed to delete object %s: %v", file.ID, err)
			}
		} else {
			c.AbortWithStatus(http.StatusUnauthorized)
		}
	})

	r.POST("/files", func(c *gin.Context) {
		authToken, err := client.VerifyIDToken(context.Background(), c.GetHeader("Authorization"))
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		header, err := c.FormFile("file")
		if err != nil {
			c.AbortWithError(http.StatusBadRequest, err)
			return
		}

		userFile, err := header.Open()
		if err != nil {
			c.AbortWithError(http.StatusBadRequest, err)
			return
		}

		id := uuid.New()

		result, err := uploader.Upload(&s3manager.UploadInput{
			Bucket:             bucket,
			Key:                aws.String(id.String()),
			Body:               userFile,
			ContentType:        aws.String(header.Header.Get("Content-Type")),
			ContentDisposition: aws.String("filename=\"" + quoteEscaper.Replace(header.Filename) + "\""),
		})
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}

		s, err := url.QueryUnescape(header.Filename)
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}

		f := File{
			ID:          id,
			UserID:      authToken.UID,
			Size:        header.Size,
			Filename:    s,
			ContentType: header.Header.Get("Content-Type"),
			Location:    result.Location,
		}

		_, err = db.Model(&f).Insert()
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}

		c.JSON(http.StatusOK, f)
	})

	r.POST("/login", func(c *gin.Context) {
		var body struct {
			From string
			To   string
		}
		if err := c.BindJSON(&body); err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		if body.From == "" {
			c.Status(http.StatusNoContent)
			return
		}

		fromAuthToken, err := client.VerifyIDToken(context.Background(), body.From)
		if err != nil || fromAuthToken.Firebase.SignInProvider != "anonymous" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		toAuthToken, err := client.VerifyIDToken(context.Background(), body.To)
		if err != nil || toAuthToken.Firebase.SignInProvider == "anonymous" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		_, err = db.Model(&File{}).
			Where("user_id = ?", fromAuthToken.UID).
			Set("user_id = ?", toAuthToken.UID).
			UpdateNotZero()
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		c.Status(http.StatusNoContent)
	})

	r.Run(":8080")
}
