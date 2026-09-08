import type { CategoryItem } from "@/lib/api"

export type CategoryNode = CategoryItem & {
  children: CategoryNode[]
}

export type FlatCategory = CategoryItem & {
  depth: number
}

function sortCategories(left: CategoryNode, right: CategoryNode) {
  return left.order_id - right.order_id || left.id - right.id
}

export function buildCategoryTree(categories: CategoryItem[]) {
  const nodes = new Map<number, CategoryNode>()
  const children = new Map<number, CategoryNode[]>()

  categories.forEach((category) => {
    nodes.set(category.id, { ...category, children: [] })
  })

  categories.forEach((category) => {
    const node = nodes.get(category.id)
    if (!node) return
    if (category.parent_id > 0 && nodes.has(category.parent_id) && category.parent_id !== category.id) {
      const siblings = children.get(category.parent_id) ?? []
      siblings.push(node)
      children.set(category.parent_id, siblings)
    }
  })

  const roots = categories
    .filter((category) => category.parent_id <= 0 || !nodes.has(category.parent_id) || category.parent_id === category.id)
    .map((category) => nodes.get(category.id))
    .filter((node): node is CategoryNode => Boolean(node))
  const visited = new Set<number>()

  function attach(node: CategoryNode) {
    if (visited.has(node.id)) return
    visited.add(node.id)
    node.children = (children.get(node.id) ?? [])
      .filter((child) => !visited.has(child.id))
      .sort(sortCategories)
    node.children.forEach(attach)
  }

  roots.sort(sortCategories).forEach(attach)
  categories.forEach((category) => {
    const node = nodes.get(category.id)
    if (node && !visited.has(node.id)) {
      roots.push(node)
      attach(node)
    }
  })

  return roots.sort(sortCategories)
}

export function flattenCategoryTree(categories: CategoryItem[], excludedID?: number) {
  const result: FlatCategory[] = []

  function visit(nodes: CategoryNode[], depth: number) {
    nodes.forEach((node) => {
      if (node.id === excludedID) return
      result.push({ ...node, depth })
      visit(node.children, depth + 1)
    })
  }

  visit(buildCategoryTree(categories), 0)
  return result
}
