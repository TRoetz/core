<script>
  import Label from './Label.svelte';
  import Input from './Input.svelte';

  /**
   * @type {{
   *   label?: string,
   *   value?: any,
   *   type?: string,
   *   placeholder?: string,
   *   required?: boolean,
   *   disabled?: boolean,
   *   autofocus?: boolean,
   *   id?: string,
   *   size?: string,
   *   min?: any,
   *   max?: any,
   *   step?: any,
   *   labelColor?: 'default' | 'subtle',
   *   labelClass?: string,
   *   dataTestid?: string,
   *   inputRef?: any,
   *   oninput?: (e?: any) => void,
   *   class?: string,
   * }}
   */
  let {
    label = '',
    value = $bindable(''),
    type = 'text',
    placeholder = '',
    required = false,
    disabled = false,
    autofocus = false,
    id = undefined,
    size = 'medium',
    min = undefined,
    max = undefined,
    step = undefined,
    labelColor = 'subtle', // 'default' | 'subtle'
    labelClass = 'mb-2',
    dataTestid = undefined,
    inputRef = $bindable(null),
    oninput = undefined,
    class: className = ''
  } = $props();
</script>

<div class={className}>
  {#if label}
    <Label for={id} {required} color={labelColor} class={labelClass}>{label}</Label>
  {/if}
  <Input
    {type}
    {id}
    bind:value
    bind:inputRef
    {placeholder}
    {required}
    {disabled}
    {autofocus}
    {size}
    {min}
    {max}
    {step}
    {dataTestid}
    {oninput}
  />
</div>
