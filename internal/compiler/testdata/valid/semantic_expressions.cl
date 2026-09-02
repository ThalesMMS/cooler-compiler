class Base {
  cached : SELF_TYPE <- new SELF_TYPE;

  identity() : SELF_TYPE { self };
  reset() : SELF_TYPE {{ cached <- new SELF_TYPE; self; }};
  conditional(flag : Bool) : Base { if flag then self else new Child fi };
  self_conditional(flag : Bool) : SELF_TYPE { if flag then self else self fi };
  arithmetic() : Int { (~1) + 2 * 3 / 1 - 4 };
  logic() : Bool { not (1 < 2) };
  equality() : Bool { "left" = "right" };
  void_test() : Bool { isvoid new Object };
  loop_expr() : Object { while false loop 0 pool };
  block_expr() : String {{ 0; "last"; }};
};

class Child inherits Base {
  child() : Int { 1 };
  chain() : Int { identity().child() };
};

class Main {
  dynamic_dispatch() : Child { (new Child).identity() };
  static_dispatch() : Base { (new Child)@Base.identity() };
  declared_dispatch() : Base { let item : Base <- new Child in item.identity() };
  sequential_let() : Int { let x : Int <- 1, y : Int <- x + 1, x : Int <- y + 1 in x };
  select(value : Base) : Base {
    case value of
      child : Child => child;
      base : Base => base;
    esac
  };
  select_reversed(value : Base) : Base {
    case value of
      base : Base => base;
      child : Child => child;
    esac
  };
  main() : Object { 0 };
};
