package cli

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/models"
)

var (
	linkFromRepo   string
	linkFromEntity string
	linkToRepo     string
	linkToEntity   string
	linkKind       string
	linkStrength   string
	linkDesc       string
)

var linkCmd = &cobra.Command{
	Use:   "link",
	Short: "Create a cross-repo relationship between entities",
	Long:  "Link entities across different repositories. Entities are resolved by file path or qualified name.",
	RunE:  runLink,
}

func init() {
	linkCmd.Flags().StringVar(&linkFromRepo, "from-repo", "", "Source repository name (required)")
	linkCmd.Flags().StringVar(&linkFromEntity, "from-entity", "", "Source entity path or qualified name (required)")
	linkCmd.Flags().StringVar(&linkToRepo, "to-repo", "", "Target repository name (required)")
	linkCmd.Flags().StringVar(&linkToEntity, "to-entity", "", "Target entity path or qualified name (required)")
	linkCmd.Flags().StringVar(&linkKind, "kind", "depends_on", "Relationship kind (depends_on, calls, implements, etc.)")
	linkCmd.Flags().StringVar(&linkStrength, "strength", "moderate", "Relationship strength (strong, moderate, weak)")
	linkCmd.Flags().StringVar(&linkDesc, "description", "", "Optional description of the relationship")

	linkCmd.MarkFlagRequired("from-repo")
	linkCmd.MarkFlagRequired("from-entity")
	linkCmd.MarkFlagRequired("to-repo")
	linkCmd.MarkFlagRequired("to-entity")

	rootCmd.AddCommand(linkCmd)
}

func runLink(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if linkFromRepo == linkToRepo {
		return fmt.Errorf("cross-repo link requires different repos; use indexing for same-repo relationships")
	}

	repoStore := &models.RepoStore{Pool: pool}
	entityStore := &models.EntityStore{Pool: pool}
	relStore := &models.RelationshipStore{Pool: pool}

	// Resolve from repo + entity
	fromRepo, err := resolveRepoByName(ctx, repoStore, linkFromRepo)
	if err != nil {
		return err
	}
	fromEntity, err := resolveEntityByPath(ctx, entityStore, fromRepo.ID, linkFromEntity)
	if err != nil {
		return fmt.Errorf("from-entity: %w", err)
	}

	// Resolve to repo + entity
	toRepo, err := resolveRepoByName(ctx, repoStore, linkToRepo)
	if err != nil {
		return err
	}
	toEntity, err := resolveEntityByPath(ctx, entityStore, toRepo.ID, linkToEntity)
	if err != nil {
		return fmt.Errorf("to-entity: %w", err)
	}

	cr := &models.CrossRepoRelationship{
		FromEntityID: fromEntity.ID,
		ToEntityID:   toEntity.ID,
		FromRepoID:   fromRepo.ID,
		ToRepoID:     toRepo.ID,
		Kind:         linkKind,
		Strength:     linkStrength,
		Provenance: []models.Provenance{{
			SourceType: "manual",
			Repo:       linkFromRepo + " → " + linkToRepo,
		}},
	}
	if linkDesc != "" {
		cr.Description = &linkDesc
	}

	if err := relStore.CreateCrossRepo(ctx, cr); err != nil {
		return fmt.Errorf("creating cross-repo relationship: %w", err)
	}

	fmt.Printf("Created cross-repo link: %s (%s) --%s--> %s (%s)\n",
		fromEntity.Name, linkFromRepo, linkKind, toEntity.Name, linkToRepo)
	return nil
}

func resolveRepoByName(ctx context.Context, store *models.RepoStore, name string) (*models.Repo, error) {
	repos, err := store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing repos: %w", err)
	}
	for _, r := range repos {
		if r.Name == name {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("repository %q not found", name)
}

func resolveEntityByPath(ctx context.Context, store *models.EntityStore, repoID uuid.UUID, path string) (*models.Entity, error) {
	e, err := store.FindByPath(ctx, repoID, path)
	if err != nil {
		return nil, err
	}
	if e != nil {
		return e, nil
	}
	e, err = store.FindByQualifiedName(ctx, repoID, path)
	if err != nil {
		return nil, err
	}
	if e != nil {
		return e, nil
	}
	return nil, fmt.Errorf("entity %q not found in repo", path)
}
