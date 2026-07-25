import { describe, expect, it } from "vitest";
import {
  findPublicArticle,
  getAllPublicArticles,
  getPublicArticlePath,
  publicSections,
} from "./PublicContentCatalog";

describe("PublicContentCatalog", () => {
  it("defines every public header destination", () => {
    expect(publicSections).toHaveLength(5);
    for (const section of publicSections) {
      expect(section.groups.flatMap((group) => group.articles)).toHaveLength(6);
    }
    expect(getAllPublicArticles()).toHaveLength(30);
  });

  it("provides unique resolvable paths", () => {
    const entries = getAllPublicArticles();
    const paths = entries.map(({ section, article }) => getPublicArticlePath(section.slug, article.slug));

    expect(new Set(paths).size).toBe(paths.length);
    for (const { section, article } of entries) {
      expect(findPublicArticle(section.slug, article.slug)?.article).toBe(article);
    }
  });
});
