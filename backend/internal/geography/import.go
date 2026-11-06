package geography

import (
	"archive/zip"
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxUpload = 64 << 20
const MaxExpanded = 512 << 20

func scanData(ctx context.Context, reader io.Reader, kind string, consume func([]string, int) error) error {
	scanner := bufio.NewScanner(io.LimitReader(reader, MaxExpanded+1))
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	total := 0
	line := 0
	for scanner.Scan() {
		line++
		raw := scanner.Text()
		total += len(raw) + 1
		if total > MaxExpanded {
			return fmt.Errorf("%w: 文件解压后过大", ErrInvalid)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		if !utf8.ValidString(raw) {
			return dataError(kind, line)
		}
		if err := consume(strings.Split(raw, "\t"), line); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%w: %s 文件无法完整读取", ErrInvalid, kind)
	}
	return nil
}
func (s *Store) importFiles(ctx context.Context, version string, countries, regions, cities io.Reader) error {
	version = strings.TrimSpace(version)
	if version == "" || len(version) > 80 {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Retain the catalogue actually in use before this import, including when
	// an administrator has previously rolled back to an older version.
	var previous sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT MAX(id) FROM geo_dataset WHERE active=1`).Scan(&previous); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO geo_dataset(version,created_at) VALUES(?,?)`, version, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	countrySet := map[string]bool{}
	var countryCount, regionCount, cityCount int
	countryStatement, err := tx.PrepareContext(ctx, `INSERT INTO geo_country(dataset,code,name) VALUES(?,?,?)`)
	if err != nil {
		return err
	}
	defer countryStatement.Close()
	if countries == nil {
		countries = strings.NewReader(seedCountries)
	}
	err = scanData(ctx, countries, "国家", func(f []string, line int) error {
		if len(f) < 17 || !validCountry(f[0]) || f[4] == "" || len(f[4]) > 256 || countryCount >= 300 {
			return dataError("国家", line)
		}
		if _, e := countryStatement.ExecContext(ctx, id, f[0], f[4]); e != nil {
			return dataError("国家", line)
		}
		countrySet[f[0]] = true
		countryCount++
		return nil
	})
	if err != nil {
		return err
	}
	if countryCount == 0 {
		return ErrInvalid
	}
	if regions != nil {
		s.Stage("导入行政区")
		stmt, e := tx.PrepareContext(ctx, `INSERT INTO geo_region(dataset,country,code,name,ascii) VALUES(?,?,?,?,?)`)
		if e != nil {
			return e
		}
		defer stmt.Close()
		err = scanData(ctx, regions, "行政区", func(f []string, line int) error {
			if len(f) < 4 {
				return dataError("行政区", line)
			}
			parts := strings.SplitN(f[0], ".", 2)
			if len(parts) != 2 || !countrySet[parts[0]] || parts[1] == "" || len(parts[1]) > 30 || f[1] == "" || len(f[1]) > 512 || len(f[2]) > 512 || regionCount >= 10000 {
				return dataError("行政区", line)
			}
			if _, e := stmt.ExecContext(ctx, id, parts[0], parts[1], f[1], f[2]); e != nil {
				return dataError("行政区", line)
			}
			regionCount++
			return nil
		})
		if err != nil {
			return err
		}
		if regionCount == 0 {
			return ErrInvalid
		}
	}
	if cities != nil {
		s.Stage("导入城市")
		stmt, e := tx.PrepareContext(ctx, `INSERT INTO geo_city(dataset,id,country,region,name,ascii,search,population) VALUES(?,?,?,?,?,?,?,?)`)
		if e != nil {
			return e
		}
		defer stmt.Close()
		err = scanData(ctx, cities, "城市", func(f []string, line int) error {
			if len(f) < 19 {
				return dataError("城市", line)
			}
			if f[6] != "P" {
				return nil
			}
			num, e := strconv.ParseInt(f[0], 10, 64)
			if e != nil || num <= 0 || f[1] == "" || len(f[1]) > 800 || len(f[2]) > 800 || len(f[3]) > 100000 || len(f[10]) > 30 || !countrySet[f[8]] || cityCount >= 1000000 {
				return dataError("城市", line)
			}
			population, e := strconv.ParseInt(f[14], 10, 64)
			if e != nil || population < 0 {
				return dataError("城市", line)
			}
			search := strings.ToLower(f[1] + " " + f[2] + " " + f[3])
			if _, e := stmt.ExecContext(ctx, id, f[0], f[8], f[10], f[1], f[2], search, population); e != nil {
				return dataError("城市", line)
			}
			cityCount++
			return nil
		})
		if err != nil {
			return err
		}
		if cityCount == 0 {
			return ErrInvalid
		}
	}
	s.Stage("切换目录版本")
	if _, err = tx.ExecContext(ctx, `UPDATE geo_dataset SET active=0 WHERE active=1`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE geo_dataset SET countries=?,regions=?,cities=?,active=1 WHERE id=?`, countryCount, regionCount, cityCount, id); err != nil {
		return err
	}
	// Keep the newly active catalogue and the previous active version.
	keep := previous.Int64
	for _, table := range []string{"geo_country", "geo_region", "geo_city"} {
		if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE dataset NOT IN (?,?)`, id, keep); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM geo_dataset WHERE id NOT IN (?,?)`, id, keep); err != nil {
		return err
	}
	return tx.Commit()
}

// Import accepts official GeoNames text files and a flat cities ZIP. Archives
// are read in place; no archive-supplied path is ever written to disk.
func (s *Store) Import(ctx context.Context, version, countriesPath, regionsPath, citiesPath string) error {
	var countries io.Reader
	if countriesPath != "" {
		file, err := os.Open(countriesPath)
		if err != nil {
			return err
		}
		defer file.Close()
		countries = file
	}
	regions, err := os.Open(regionsPath)
	if err != nil {
		return err
	}
	defer regions.Close()
	file, err := os.Open(citiesPath)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() > MaxUpload {
		return ErrInvalid
	}
	header := make([]byte, 4)
	n, _ := file.ReadAt(header, 0)
	if n == 4 && string(header) == "PK\x03\x04" {
		archive, err := zip.NewReader(file, info.Size())
		if err != nil {
			return ErrInvalid
		}
		if len(archive.File) > 4 {
			return ErrInvalid
		}
		var entry *zip.File
		for _, candidate := range archive.File {
			if candidate.FileInfo().IsDir() {
				continue
			}
			if filepath.Base(candidate.Name) != candidate.Name || strings.ContainsAny(candidate.Name, "\\/") || candidate.Mode()&os.ModeSymlink != 0 || candidate.UncompressedSize64 > MaxExpanded {
				return ErrInvalid
			}
			if strings.HasSuffix(strings.ToLower(candidate.Name), ".txt") {
				if entry != nil {
					return ErrInvalid
				}
				entry = candidate
			}
		}
		if entry == nil {
			return ErrInvalid
		}
		reader, err := entry.Open()
		if err != nil {
			return ErrInvalid
		}
		defer reader.Close()
		return s.importFiles(ctx, version, countries, regions, reader)
	}
	return s.importFiles(ctx, version, countries, regions, file)
}

// Download is deliberately restricted to three fixed official HTTPS resources.
// User-supplied URLs and redirects are not accepted.
func (s *Store) Download(ctx context.Context) error {
	dir, err := os.MkdirTemp("", "gocms-geography-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	client := http.Client{Timeout: 3 * time.Minute, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	for _, name := range []string{"countryInfo.txt", "admin1CodesASCII.txt", "cities500.zip"} {
		s.Stage("下载 " + name)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://download.geonames.org/export/dump/"+name, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("GeoNames 下载失败，请重试或上传官方文件")
		}
		if response.StatusCode != 200 {
			response.Body.Close()
			return fmt.Errorf("GeoNames 下载失败（HTTP %d），可改用文件上传", response.StatusCode)
		}
		target, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			response.Body.Close()
			return err
		}
		size, copyErr := io.Copy(target, io.LimitReader(response.Body, MaxUpload+1))
		closeErr := target.Close()
		response.Body.Close()
		if copyErr != nil || closeErr != nil || size > MaxUpload {
			return fmt.Errorf("GeoNames 下载文件不完整或超过大小限制")
		}
	}
	return s.Import(ctx, "GeoNames "+time.Now().UTC().Format("2006-01-02 15:04"), filepath.Join(dir, "countryInfo.txt"), filepath.Join(dir, "admin1CodesASCII.txt"), filepath.Join(dir, "cities500.zip"))
}
