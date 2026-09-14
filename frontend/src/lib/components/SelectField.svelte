<script>
  import Label from './Label.svelte';
  import Select from './Select.svelte';

  /**
   * @type {{
   *   label?: string,
   *   value?: any,
   *   options?: Array<any>,
   *   placeholder?: string,
   *   required?: boolean,
   *   disabled?: boolean,
   *   id?: string,
   *   size?: string,
   *   labelColor?: 'default' | 'subtle',
   *   labelClass?: string,
   *   ariaLabel?: string,
   *   onchange?: (e?: any) => void,
   *   class?: string,
   * }}
   */
  let {
    label = '',
    value = $bindable(''),
    options = [],
    placeholder = '',
    required = false,
    disabled = false,
    id = undefined,
    size = 'medium',
    labelColor = 'subtle',
    labelClass = 'mb-2',
    ariaLabel = undefined,
    onchange = undefined,
    class: className = ''
  } = $props();
</script>

<div class={className}>
  {#if label}
    <Label for={id} {required} color={labelColor} class={labelClass}>{label}</Label>
  {/if}
  <Select
    {id}
    bind:value
    {options}
    {placeholder}
    {required}
    {disabled}
    {size}
    {ariaLabel}
    {onchange}
  />
</div>
