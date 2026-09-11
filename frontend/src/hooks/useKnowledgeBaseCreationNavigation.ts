import { useRouter } from 'vue-router'

/**
 * Provides a shared navigation helper for knowledge-base creation success.
 * Redirects to the new knowledge base so adding the first document is the
 * immediate next action.
 */
export const useKnowledgeBaseCreationNavigation = () => {
  const router = useRouter()

  const navigateToKnowledgeBaseList = (kbId: string) => {
    if (!kbId) return
    router.push(`/platform/knowledge-bases/${kbId}`)
  }

  return {
    navigateToKnowledgeBaseList,
  }
}

