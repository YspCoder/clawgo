import React from 'react';

type ButtonVariant = 'neutral' | 'primary' | 'accent' | 'success' | 'warning' | 'danger';
type FixedButtonShape = 'icon' | 'square';
type ButtonSize = 'sm' | 'md' | 'xs' | 'xs_tall' | 'md_tall' | 'md_wide';
type ButtonRadius = 'default' | 'lg' | 'xl' | 'full';
type ButtonGap = 'none' | '1' | '2';
type NativeButtonProps = Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, 'className'>;
type NativeAnchorProps = Omit<React.AnchorHTMLAttributes<HTMLAnchorElement>, 'className'>;

type ButtonStyleProps = {
  radius?: ButtonRadius;
  gap?: ButtonGap;
  shadow?: boolean;
  noShrink?: boolean;
};

type ButtonProps = NativeButtonProps & ButtonStyleProps & {
  variant?: ButtonVariant;
  size?: ButtonSize;
  fullWidth?: boolean;
  grow?: boolean;
};

type LinkButtonProps = NativeAnchorProps & ButtonStyleProps & {
  variant?: ButtonVariant;
  size?: ButtonSize;
  fullWidth?: boolean;
  grow?: boolean;
};

type FixedButtonProps = NativeButtonProps & ButtonStyleProps & {
  label: string;
  shape?: FixedButtonShape;
  variant?: ButtonVariant;
};

type FixedLinkButtonProps = NativeAnchorProps & ButtonStyleProps & {
  label: string;
  shape?: FixedButtonShape;
  variant?: ButtonVariant;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

function buttonSizeClass(size: ButtonSize) {
  switch (size) {
    case 'xs':
      return 'px-2.5 py-1 text-xs';
    case 'xs_tall':
      return 'px-2.5 py-2 text-xs';
    case 'sm':
      return 'px-3 py-1.5 text-sm';
    case 'md_tall':
      return 'px-4 py-2.5 text-sm font-medium';
    case 'md_wide':
      return 'px-6 py-2 text-sm font-medium';
    case 'md':
    default:
      return 'px-4 py-2 text-sm font-medium';
  }
}

function buttonRadiusClass(radius: ButtonRadius) {
  switch (radius) {
    case 'lg':
      return 'rounded-lg';
    case 'xl':
      return 'rounded-xl';
    case 'full':
      return 'rounded-full';
    case 'default':
    default:
      return undefined;
  }
}

function buttonGapClass(gap: ButtonGap) {
  switch (gap) {
    case '1':
      return 'gap-1';
    case '2':
      return 'gap-2';
    case 'none':
    default:
      return undefined;
  }
}

function buttonClass(
  variant: ButtonVariant,
  size: ButtonSize,
  fullWidth: boolean,
  grow: boolean,
  radius: ButtonRadius,
  gap: ButtonGap,
  shadow: boolean,
  noShrink: boolean,
) {
  return joinClasses(
    'ui-button',
    `ui-button-${variant}`,
    buttonSizeClass(size),
    fullWidth && 'w-full',
    grow && 'flex-1',
    buttonRadiusClass(radius),
    buttonGapClass(gap),
    shadow && 'shadow-sm',
    noShrink && 'shrink-0',
  );
}

export function Button({
  variant = 'neutral',
  size = 'md',
  fullWidth = false,
  grow = false,
  radius = 'default',
  gap = 'none',
  shadow = false,
  noShrink = false,
  type = 'button',
  ...props
}: ButtonProps) {
  return <button {...props} type={type} className={buttonClass(variant, size, fullWidth, grow, radius, gap, shadow, noShrink)} />;
}

export function LinkButton({
  variant = 'neutral',
  size = 'md',
  fullWidth = false,
  grow = false,
  radius = 'default',
  gap = 'none',
  shadow = false,
  noShrink = false,
  ...props
}: LinkButtonProps) {
  return <a {...props} className={buttonClass(variant, size, fullWidth, grow, radius, gap, shadow, noShrink)} />;
}

export function FixedButton({
  label,
  shape = 'icon',
  variant = 'neutral',
  radius = 'default',
  gap = 'none',
  shadow = false,
  noShrink = false,
  type = 'button',
  title,
  children,
  ...props
}: FixedButtonProps) {
  return (
    <button
      {...props}
      type={type}
      title={title || label}
      aria-label={props['aria-label'] || label}
      className={joinClasses(
        'ui-button',
        `ui-button-${variant}`,
        shape === 'square' ? 'ui-button-square' : 'ui-button-icon',
        buttonRadiusClass(radius),
        buttonGapClass(gap),
        shadow && 'shadow-sm',
        noShrink && 'shrink-0',
      )}
    >
      {children}
    </button>
  );
}

export function FixedLinkButton({
  label,
  shape = 'icon',
  variant = 'neutral',
  radius = 'default',
  gap = 'none',
  shadow = false,
  noShrink = false,
  title,
  children,
  ...props
}: FixedLinkButtonProps) {
  return (
    <a
      {...props}
      title={title || label}
      aria-label={props['aria-label'] || label}
      className={joinClasses(
        'ui-button',
        `ui-button-${variant}`,
        shape === 'square' ? 'ui-button-square' : 'ui-button-icon',
        buttonRadiusClass(radius),
        buttonGapClass(gap),
        shadow && 'shadow-sm',
        noShrink && 'shrink-0',
      )}
    >
      {children}
    </a>
  );
}
