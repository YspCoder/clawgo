import React from 'react';

type InputProps = React.InputHTMLAttributes<HTMLInputElement>;

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const Input = React.forwardRef<HTMLInputElement, InputProps>(function Input(
  { className, ...props },
  ref,
) {
  return (
    <input
      {...props}
      ref={ref}
      className={joinClasses('ui-input', className)}
    />
  );
});

export default Input;
export type { InputProps };
