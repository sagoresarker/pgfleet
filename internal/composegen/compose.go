// Package composegen builds docker-compose YAML that reproduces how PgFleet runs
// a managed instance — or a whole cluster (primary + replicas + PgCat router) —
// so operators can download a compose file and run it themselves.
//
// Everything here is pure string building: no Docker, DB, or filesystem calls.
// The real superuser password is NEVER emitted; the generated file references a
// ${POSTGRES_PASSWORD} placeholder the operator supplies via a .env file.
package composegen

import (
	"fmt"
	"strings"
)

// containerName mirrors provision.InstanceContainerName ("pgfleet-pg-<name>").
// It is duplicated (not imported) so this package stays free of the provision
// dependency graph (Docker/DB), keeping it a pure, fast-to-test string builder.
func containerName(name string) string { return "pgfleet-pg-" + name }

// dataVolume / repoVolume mirror provision.volumeName ("pgfleet-<kind>-<id>"),
// but keyed by instance NAME here (the compose file is human-run, so volumes are
// named after the instance the operator knows, not its opaque DB id).
func dataVolume(name string) string { return "pgfleet-data-" + name }
func repoVolume(name string) string { return "pgfleet-repo-" + name }

// routerName mirrors the router container name ("pgfleet-router-<cluster>").
func routerName(cluster string) string { return "pgfleet-router-" + cluster }

// InstanceComposeInput is the plain, domain-free view of one instance the API
// layer maps an instance.Instance onto.
type InstanceComposeInput struct {
	Name      string // instance name (service/volume basis)
	PGVersion string // Postgres major version, for the header comment
	Image     string // managed-instance image (pgfleet/postgres-pgbackrest:<v>)
	Port      int    // container Postgres port (5432); also used host-side
	RepoType  string // "local" | "s3"; "local" gets a named repo volume
	Superuser string // POSTGRES_USER value
}

// ClusterComposeInput is the plain view of a cluster the API layer maps a
// cluster + its member instances onto.
type ClusterComposeInput struct {
	Name        string   // cluster name (router/header basis)
	PrimaryName string   // primary instance name
	PGVersion   string   // Postgres major version (members share it)
	Image       string   // managed-instance image for the members
	Port        int      // container Postgres port (5432)
	RepoType    string   // "local" | "s3"
	Superuser   string   // POSTGRES_USER value
	Replicas    []string // replica instance names (may be empty)
	PgCatImage  string   // router image (ghcr.io/postgresml/pgcat:latest)
	RouterPort  int      // router listen port (6432)
}

// InstanceCompose returns a docker-compose v3 YAML document for a single
// instance: one service named like its container, the managed image, a
// ${POSTGRES_PASSWORD} placeholder, the Postgres port mapping, named data (and,
// for a local repo, backup) volumes, a restart policy, and a pg_isready
// healthcheck.
func InstanceCompose(in InstanceComposeInput) string {
	port := in.Port
	if port == 0 {
		port = 5432
	}
	var b strings.Builder
	writeHeader(&b, in.Name, in.PGVersion)
	b.WriteString("# set POSTGRES_PASSWORD in a .env file next to this compose file.\n")
	b.WriteString("version: \"3.8\"\n\n")
	b.WriteString("services:\n")
	writeInstanceService(&b, in.Name, in.Image, in.Superuser, in.RepoType, port, port, dependsOn(nil))
	b.WriteString("\n")

	b.WriteString("volumes:\n")
	for _, v := range instanceVolumes(in.Name, in.RepoType) {
		fmt.Fprintf(&b, "  %s:\n", v)
	}
	return b.String()
}

// ClusterCompose returns a docker-compose v3 YAML document for a cluster: the
// primary, each replica (depending on the primary), and a PgCat router fronting
// them. Router config beyond the service stub is out of scope — a comment notes
// the router needs its generated pgcat.toml.
func ClusterCompose(in ClusterComposeInput) string {
	port := in.Port
	if port == 0 {
		port = 5432
	}
	routerPort := in.RouterPort
	if routerPort == 0 {
		routerPort = 6432
	}

	var b strings.Builder
	writeHeader(&b, in.Name, in.PGVersion)
	b.WriteString("# set POSTGRES_PASSWORD in a .env file next to this compose file.\n")
	b.WriteString("version: \"3.8\"\n\n")
	b.WriteString("services:\n")

	// Primary first; replicas stream from it, so they depend_on it. Each member's
	// Postgres listens on the same CONTAINER port (5432), but they must publish on
	// DISTINCT HOST ports — otherwise `docker compose up` fails with "port is
	// already allocated" the moment a cluster has ≥1 replica.
	writeInstanceService(&b, in.PrimaryName, in.Image, in.Superuser, in.RepoType, port, port, dependsOn(nil))
	b.WriteString("\n")
	for i, r := range in.Replicas {
		writeInstanceService(&b, r, in.Image, in.Superuser, in.RepoType, port+1+i, port,
			dependsOn([]string{containerName(in.PrimaryName)}))
		b.WriteString("\n")
	}

	writeRouterService(&b, in.Name, in.PrimaryName, in.Image, in.PgCatImage, routerPort, in.Replicas)
	b.WriteString("\n")

	b.WriteString("volumes:\n")
	for _, name := range append([]string{in.PrimaryName}, in.Replicas...) {
		for _, v := range instanceVolumes(name, in.RepoType) {
			fmt.Fprintf(&b, "  %s:\n", v)
		}
	}
	return b.String()
}

// writeHeader writes the "Generated by PgFleet" banner naming the subject and
// Postgres version.
func writeHeader(b *strings.Builder, name, pgVersion string) {
	fmt.Fprintf(b, "# Generated by PgFleet for %q (pg version %s)\n", name, pgVersion)
	b.WriteString("# Reproduces how PgFleet runs this; review before use.\n")
}

// dependsOn returns the depends_on service list (may be empty/nil).
func dependsOn(names []string) []string { return names }

// writeInstanceService writes one Postgres service block (2-space indent under
// services:). The password is always the ${POSTGRES_PASSWORD} placeholder.
func writeInstanceService(b *strings.Builder, name, image, superuser, repoType string, hostPort, containerPort int, deps []string) {
	svc := containerName(name)
	su := superuser
	if su == "" {
		su = "postgres"
	}
	fmt.Fprintf(b, "  %s:\n", svc)
	fmt.Fprintf(b, "    image: %s\n", image)
	fmt.Fprintf(b, "    container_name: %s\n", svc)
	b.WriteString("    restart: unless-stopped\n")
	b.WriteString("    environment:\n")
	fmt.Fprintf(b, "      POSTGRES_USER: %s\n", su)
	// Never emit the real password — only the placeholder.
	b.WriteString("      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}\n")
	b.WriteString("      POSTGRES_DB: postgres\n")
	b.WriteString("    ports:\n")
	fmt.Fprintf(b, "      - \"%d:%d\"\n", hostPort, containerPort)
	b.WriteString("    volumes:\n")
	fmt.Fprintf(b, "      - %s:/var/lib/postgresql/data\n", dataVolume(name))
	// A local pgBackRest repo lives on a named volume; an S3 repo has none.
	if repoType != "s3" {
		fmt.Fprintf(b, "      - %s:/var/lib/pgbackrest\n", repoVolume(name))
	}
	if len(deps) > 0 {
		b.WriteString("    depends_on:\n")
		for _, d := range deps {
			fmt.Fprintf(b, "      - %s\n", d)
		}
	}
	b.WriteString("    healthcheck:\n")
	fmt.Fprintf(b, "      test: [\"CMD-SHELL\", \"pg_isready -U %s\"]\n", su)
	b.WriteString("      interval: 10s\n")
	b.WriteString("      timeout: 5s\n")
	b.WriteString("      retries: 5\n")
}

// writeRouterService writes the PgCat router service stub. The full pgcat.toml
// is out of scope; a comment tells the operator it must be supplied.
func writeRouterService(b *strings.Builder, cluster, primaryName, _ /*image*/, pgcatImage string, routerPort int, replicas []string) {
	fmt.Fprintf(b, "  %s:\n", routerName(cluster))
	fmt.Fprintf(b, "    image: %s\n", pgcatImage)
	fmt.Fprintf(b, "    container_name: %s\n", routerName(cluster))
	b.WriteString("    restart: unless-stopped\n")
	b.WriteString("    # The router needs its generated pgcat.toml mounted at /etc/pgcat/pgcat.toml\n")
	b.WriteString("    # (PgFleet generates it with the cluster's credentials and member backends).\n")
	b.WriteString("    ports:\n")
	fmt.Fprintf(b, "      - \"%d:%d\"\n", routerPort, routerPort)
	b.WriteString("    depends_on:\n")
	fmt.Fprintf(b, "      - %s\n", containerName(primaryName))
	for _, r := range replicas {
		fmt.Fprintf(b, "      - %s\n", containerName(r))
	}
}

// instanceVolumes returns the named volumes an instance declares: always data,
// plus a backup-repo volume only for a local repo (S3 has no local repo).
func instanceVolumes(name, repoType string) []string {
	vols := []string{dataVolume(name)}
	if repoType != "s3" {
		vols = append(vols, repoVolume(name))
	}
	return vols
}
