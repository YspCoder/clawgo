import React from 'react';

type SelectProps = React.SelectHTMLAttributes<HTMLSelectElement>;

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const Select = React.forwardRef<HTMLSelectElement, SelectProps>(function Select(
  { className, children, ...props },
  ref,
) {
  return (
    <select
      {...props}
      ref={ref}
      className={joinClasses('ui-select', className)}
    >
      {children}
    </select>
  );
});

export default Select;
export type { SelectProps };
