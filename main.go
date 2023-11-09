package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	firebase "firebase.google.com/go/v4"
	"github.com/davecgh/go-spew/spew"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	"github.com/go-pg/pg/v10/orm"
	"github.com/google/uuid"
	"google.golang.org/api/option"
)

const FILES = "./files"

type File struct {
	ID          uuid.UUID `json:"id" pg:",type:uuid"`
	UserID      string    `json:"user_id" pg:",notnull"`
	CreatedAt   time.Time `json:"created_at" pg:"default:now()"`
	Size        int64     `json:"size"`
	Filename    string    `json:"name"`
	ContentType string    `json:"type"`
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
	app, err := firebase.NewApp(context.Background(), nil, option.WithCredentialsFile("private_keys.json"))
	if err != nil {
		panic(err)
	}

	client, err := app.Auth(context.Background())
	if err != nil {
		panic(err)
	}

	r := gin.Default()
	db = pg.Connect(&pg.Options{
		User:     "postgres",
		Password: "epic",
	})
	defer db.Close()
	db.AddQueryHook(dbLogger{})

	models := []interface{}{
		(*File)(nil),
	}

	for _, model := range models {
		db.Model(model).DropTable(&orm.DropTableOptions{IfExists: true})
		err := db.Model(model).CreateTable(&orm.CreateTableOptions{
			IfNotExists: true,
		})
		if err != nil {
			panic(err)
		}
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_user_id ON files (user_id)`); err != nil {
		panic(err)
	}

	r.Use(static.Serve("/", static.LocalFile("static", false)))

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

		c.Header("Content-Type", file.ContentType)
		c.Header("Content-Disposition", "filename=\""+quoteEscaper.Replace(file.Filename)+"\"")
		c.File(filepath.Join(FILES, file.ID.String()))
	})

	r.POST("/files", func(c *gin.Context) {
		authToken, err := client.VerifyIDToken(context.Background(), c.Request.FormValue("token"))
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		header, err := c.FormFile("file")
		if err != nil {
			c.AbortWithError(http.StatusBadRequest, err)
			return
		}

		spew.Dump(header.Header)

		userFile, err := header.Open()
		if err != nil {
			c.AbortWithError(http.StatusBadRequest, err)
			return
		}

		fmt.Println("copying " + header.Filename)

		id := uuid.New()
		localFile, err := os.Create(filepath.Join(FILES, id.String()))
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}

		_, err = io.Copy(localFile, userFile)
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
		}

		_, err = db.Model(&f).Insert()
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}

		c.JSON(http.StatusOK, f)
	})

	r.Run(":8080")
}
