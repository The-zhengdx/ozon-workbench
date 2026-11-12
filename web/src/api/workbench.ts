import { http } from "./http";
import type {
  AttributeSnapshot,
  CategoryTree,
  DictPage,
  EventPage,
  PromptRecord,
  Shop,
  ShopWriteResult,
  TaskPage,
  TreeNode,
  WorkbenchTask,
} from "./types";

export async function fetchMeta() {
  const { data } = await http.get<{ service: string; mock: boolean; proxy?: string; codex_mock?: boolean; phase: number }>("/meta");
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

export async function listTasks(status = "", page = 1, pageSize = 50) {
  const { data } = await http.get<TaskPage>("/tasks", { params: { status, page, page_size: pageSize } });
  return data;
}

export async function getTask(id: number) {
  const { data } = await http.get<WorkbenchTask>(`/tasks/${id}`);
  return data;
}

export async function createTask(form: FormData, onProgress?: (percent: number) => void) {
  const { data } = await http.post<WorkbenchTask>("/tasks", form, {
    timeout: 0,
    onUploadProgress(event) {
      if (!onProgress) return;
      const total = event.total ?? 0;
      onProgress(total > 0 ? Math.min(100, Math.round((event.loaded / total) * 100)) : 0);
    },
  });
  return data;
}

export async function updateTaskPolicy(id: number, body: { revision: number; auto_text_approve: boolean; auto_visual_approve: boolean }) {
  const { data } = await http.post<WorkbenchTask>(`/tasks/${id}/policy`, body);
  return data;
}

export async function approveTask(id: number, body: { revision: number; stage: "text" | "visual" }) {
  const { data } = await http.post<WorkbenchTask>(`/tasks/${id}/approve`, body);
  return data;
}

export async function submitTaskInput(id: number, body: { revision: number; fields: Record<string, string> }) {
  const { data } = await http.post<WorkbenchTask>(`/tasks/${id}/input`, body);
  return data;
}

export async function followUpTask(id: number, body: { revision: number; content: string }) {
  const { data } = await http.post<WorkbenchTask>(`/tasks/${id}/messages`, body);
  return data;
}

export async function listTaskEvents(id: number, params: { after_seq?: number; before_seq?: number; limit?: number } = {}) {
  const { data } = await http.get<EventPage>(`/tasks/${id}/events`, { params });
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
