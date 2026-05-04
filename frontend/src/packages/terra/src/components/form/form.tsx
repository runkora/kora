import * as React from 'react';
import { cn } from '../../utils';
import { GAP } from '../layout/spacing';
import { MAX_W } from '../layout/sizing';
import type { SpacingSize } from '../layout/spacing';
import type { ContainerWidth } from '../layout/sizing';

export interface FormProps {
  children: React.ReactNode;
  onSubmit?: React.FormEventHandler<HTMLFormElement>;
  noValidate?: boolean;
  gap?: SpacingSize;
  maxWidth?: ContainerWidth;
  id?: string;
  'aria-label'?: string;
  'aria-labelledby'?: string;
}

/**
 * Form
 *
 * A `<form>` element styled to the Terra token system. Renders a vertically
 * stacked flex container with token-aligned gap between fields.
 *
 * ```tsx
 * <Form onSubmit={handleSubmit} gap="comfortable">
 *   <FormField label="Name" id="name">
 *     <Input id="name" />
 *   </FormField>
 *   <Button type="submit">Save</Button>
 * </Form>
 * ```
 */
const Form = React.forwardRef<HTMLFormElement, FormProps>(
  ({ children, onSubmit, noValidate, gap = 'snug', maxWidth, id, ...props }, ref) => {
    return (
      <form
        ref={ref}
        id={id}
        onSubmit={onSubmit}
        noValidate={noValidate}
        className={cn(
          'flex flex-col',
          GAP.base[gap],
          maxWidth && MAX_W.base[maxWidth],
          maxWidth && 'w-full mx-auto',
        )}
        {...props}
      >
        {children}
      </form>
    );
  },
);

Form.displayName = 'Form';

export { Form };
