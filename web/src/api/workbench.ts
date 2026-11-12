import { http } from "./http";
import type {
  AttributeSnapshot,
  CategoryTree,
  DictPage,
  PromptRecord,
  Shop,
  ShopWriteResult,
  TreeNode,
} from "./types";

export async function fetchMeta() {
  const { data } = await http.get<{ service: string; mock: boolean; proxy: string; phase: number }>("/meta");
  return data;
}

export async function listShops() {
  const { data } = await http.get<{ shops: Shop[] }>("/shops");
  return data.shops ?? [];
}

export async function createShop(body: { name: string; client_id: string; api_key: string }) {
  const { data } = await http.post<ShopWriteResult>("/shops", body);
  return data;
}

export async function updateShop(id: number, body: { name: string; client_id: string; api_key: string }) {
  const { data } = await http.put<ShopWriteResult>(`/shops/${id}`, body);
  return data;
}

export async function archiveShop(id: number) {
  const { data } = await http.delete<Shop>(`/shops/${id}`);
  return data;
}

export async function restoreShop(id: number) {
  const { data } = await http.post<Shop>(`/shops/${id}/restore`);
  return data;
}

export async function setDefaultShop(id: number) {
  const { data } = await http.post<Shop>(`/shops/${id}/default`);
  return data;
}

export async function testShop(id: number) {
  const { data } = await http.post<ShopWriteResult>(`/shops/${id}/connection-test`);
  return data;
}

export async function fetchTree(language: string) {
  const { data } = await http.get<CategoryTree>("/categories/tree", { params: { language } });
  return data;
}

export async function syncTree(language: string) {
  const { data } = await http.post<CategoryTree>("/categories/sync", { language });
  return data;
}

export async function importTree(language: string, payload: unknown) {
  const { data } = await http.post<CategoryTree>("/categories/import", { language, payload });
  return data;
}

export async function searchNodes(language: string, q: string) {
  const { data } = await http.get<{ nodes: TreeNode[] }>("/categories/search", { params: { language, q } });
  return data.nodes ?? [];
}

export async function fetchAttributes(descriptionCategoryId: number, typeId: number, language: string) {
  const { data } = await http.get<AttributeSnapshot>("/categories/attributes", {
    params: { description_category_id: descriptionCategoryId, type_id: typeId, language },
  });
  return data;
}

export async function syncAttributes(descriptionCategoryId: number, typeId: number, language: string) {
  const { data } = await http.post<AttributeSnapshot>("/categories/attributes/sync", {
    description_category_id: descriptionCategoryId,
    type_id: typeId,
    language,
  });
  return data;
}

export async function importAttributes(descriptionCategoryId: number, typeId: number, language: string, payload: unknown) {
  const { data } = await http.post<AttributeSnapshot>("/categories/attributes/import", {
    description_category_id: descriptionCategoryId,
    type_id: typeId,
    language,
    payload,
  });
  return data;
}

export async function fetchPrompt(descriptionCategoryId: number, typeId: number) {
  const { data } = await http.get<PromptRecord>("/category-prompts", {
    params: { description_category_id: descriptionCategoryId, type_id: typeId },
  });
  return data;
}

export async function savePrompt(descriptionCategoryId: number, typeId: number, codexPrompt: string) {
  const { data } = await http.put<PromptRecord>("/category-prompts", {
    description_category_id: descriptionCategoryId,
    type_id: typeId,
    codex_prompt: codexPrompt,
  });
  return data;
}

export async function fetchDictValues(params: {
  description_category_id: number;
  type_id: number;
  attribute_id: number;
  language: string;
  limit?: number;
  last_value_id?: number;
  refresh?: boolean;
}) {
  const { data } = await http.get<DictPage>("/categories/attribute-values", {
    params: { ...params, refresh: params.refresh ? "true" : "false" },
  });
  return data;
}

export async function searchDictValues(body: {
  description_category_id: number;
  type_id: number;
  attribute_id: number;
  language: string;
  value: string;
  limit?: number;
  refresh?: boolean;
}) {
  const { data } = await http.post<DictPage>("/categories/attribute-values/search", body);
  return data;
}
