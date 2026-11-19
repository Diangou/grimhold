// Package open5e est un client minimal pour l'API Open5e (D&D 5e SRD).
package open5e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBaseURL est la racine de l'API v1 ; les endpoints monsters/ et
	// weapons/ en sont dérivés.
	DefaultBaseURL = "https://api.open5e.com/v1/"
	// DefaultTimeout est le délai maximal d'une requête HTTP.
	DefaultTimeout = 10 * time.Second
	// SRDDocument filtre les résultats sur le SRD officiel (évite les doublons).
	SRDDocument = "wotc-srd"
	// DefaultPageSize est le nombre de monstres par page du bestiaire.
	DefaultPageSize = 20

	// maxBodySize borne la taille d'une réponse lue en mémoire.
	maxBodySize = 4 << 20
	// maxWeaponPages évite une boucle infinie si l'API renvoyait des pages
	// "next" qui se répondent.
	maxWeaponPages = 20

	monstersPath = "monsters/"
	weaponsPath  = "weapons/"
)

// ErrNotFound est renvoyée quand l'API répond 404 pour un monstre.
var ErrNotFound = errors.New("monstre inconnu")

// errHTTPNotFound est l'erreur brute d'un 404 ; chaque méthode la traduit
// en erreur métier (ex. ErrNotFound pour un monstre).
var errHTTPNotFound = errors.New("ressource introuvable (HTTP 404)")

// Client interroge Open5e en passant systématiquement par un Cache.
type Client struct {
	baseURL  *url.URL
	http     *http.Client
	cache    *Cache
	pageSize int
}

// ClientOption personnalise un Client.
type ClientOption func(*Client)

// WithHTTPClient remplace le client HTTP (ex. httptest pour les tests).
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) {
		if hc != nil {
			c.http = hc
		}
	}
}

// WithPageSize change la taille des pages du bestiaire.
func WithPageSize(n int) ClientOption {
	return func(c *Client) {
		if n > 0 {
			c.pageSize = n
		}
	}
}

// NewClient construit un client pour baseURL, la racine de l'API (ex.
// DefaultBaseURL ou l'URL d'un serveur de test). Le cache est obligatoire : toutes les requêtes y passent.
func NewClient(baseURL string, cache *Cache, opts ...ClientOption) (*Client, error) {
	if cache == nil {
		return nil, errors.New("open5e: cache requis")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("open5e: URL de base %q: %w", baseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("open5e: URL de base %q: schéma http(s) attendu", baseURL)
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	c := &Client{
		baseURL:  u,
		http:     &http.Client{Timeout: DefaultTimeout},
		cache:    cache,
		pageSize: DefaultPageSize,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// PageSize renvoie le nombre de monstres demandés par page.
func (c *Client) PageSize() int { return c.pageSize }

// FirstPageURL renvoie l'URL de la première page du bestiaire (SRD uniquement).
func (c *Client) FirstPageURL() string {
	return c.listURL(monstersPath, c.pageSize)
}

// listURL construit l'URL d'une liste filtrée sur le SRD.
func (c *Client) listURL(path string, limit int) string {
	u := c.baseURL.ResolveReference(&url.URL{Path: path})
	q := url.Values{}
	q.Set("document__slug", SRDDocument)
	q.Set("format", "json")
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()
	return u.String()
}

// ListMonsters récupère la page de monstres située à pageURL (FirstPageURL,
// ou les champs Next/Previous d'une page précédente).
func (c *Client) ListMonsters(ctx context.Context, pageURL string) (MonsterList, error) {
	var list MonsterList
	body, err := c.get(ctx, pageURL)
	if err != nil {
		return list, fmt.Errorf("liste des monstres: %w", err)
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return list, fmt.Errorf("liste des monstres: décodage JSON: %w", err)
	}
	return list, nil
}

// GetMonster récupère la fiche d'un monstre par son slug.
// Un 404 renvoie une erreur enveloppant ErrNotFound.
func (c *Client) GetMonster(ctx context.Context, slug string) (Monster, error) {
	var m Monster
	if !ValidSlug(slug) {
		return m, fmt.Errorf("slug %q invalide: %w", slug, ErrNotFound)
	}
	// Django attend le slash final (sinon redirection 301).
	u := c.baseURL.ResolveReference(&url.URL{Path: monstersPath + slug + "/"})
	u.RawQuery = url.Values{"format": {"json"}}.Encode()

	body, err := c.get(ctx, u.String())
	if errors.Is(err, errHTTPNotFound) {
		return m, fmt.Errorf("monstre %q: %w", slug, ErrNotFound)
	}
	if err != nil {
		return m, fmt.Errorf("monstre %q: %w", slug, err)
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return m, fmt.Errorf("monstre %q: décodage JSON: %w", slug, err)
	}
	if m.Slug == "" {
		return m, fmt.Errorf("monstre %q: réponse sans slug", slug)
	}
	return m, nil
}

// ListWeapons renvoie toutes les armes du SRD, en suivant la pagination.
func (c *Client) ListWeapons(ctx context.Context) ([]Weapon, error) {
	var all []Weapon
	next := c.listURL(weaponsPath, 100)
	for page := 1; next != ""; page++ {
		if page > maxWeaponPages {
			return nil, fmt.Errorf("liste des armes: plus de %d pages", maxWeaponPages)
		}
		body, err := c.get(ctx, next)
		if err != nil {
			return nil, fmt.Errorf("liste des armes (page %d): %w", page, err)
		}
		var list WeaponList
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, fmt.Errorf("liste des armes (page %d): décodage JSON: %w", page, err)
		}
		all = append(all, list.Results...)
		next = ""
		if list.Next != nil {
			next = *list.Next
		}
	}
	return all, nil
}

// get renvoie le corps de la réponse à rawURL, depuis le cache si possible.
// Seules les réponses 200 sont mises en cache.
func (c *Client) get(ctx context.Context, rawURL string) (body []byte, err error) {
	if cached, ok := c.cache.Get(rawURL); ok {
		return cached, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("création de la requête: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "grimhold/1.0 (Go)")

	resp, err := c.http.Do(req)
	if err != nil {
		// *url.Error contient déjà la méthode et l'URL.
		return nil, fmt.Errorf("requête HTTP: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("fermeture de la réponse: %w", cerr)
		}
	}()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, errHTTPNotFound
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GET %s: statut HTTP inattendu %s", rawURL, resp.Status)
	}

	body, err = io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("lecture de la réponse: %w", err)
	}
	c.cache.Add(rawURL, body)
	return body, nil
}
