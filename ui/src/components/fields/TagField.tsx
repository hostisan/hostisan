import { TagInput, type TagInputProps } from '@cloudflare/kumo'
import { useFormGuard } from '../../unsaved/types'

/**
 * Kumo TagInput that tells the form page it changed. Removing a tag fires no native
 * input or change event, so FormPage can't see it without this.
 */
export function TagField({ onValueChange, ...props }: TagInputProps) {
  const guard = useFormGuard()
  return (
    <TagInput
      {...props}
      onValueChange={(value) => {
        guard?.markDirty()
        onValueChange?.(value)
      }}
    />
  )
}
